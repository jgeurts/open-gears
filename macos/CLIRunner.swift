import Darwin
import Foundation

struct CLIResult: Sendable {
    let output: Data
    let diagnostic: String
    let succeeded: Bool
    let timedOut: Bool
}

enum CLIError: LocalizedError {
    case missingHelper(String)
    case launch(String)
    case timedOut
    case command(String)

    var errorDescription: String? {
        switch self {
        case .missingHelper(let path):
            return "The Open Gears command-line helper is missing or not executable at \(path). Rebuild the app, or set OPEN_GEARS_CLI when developing."
        case .launch(let message):
            return "Could not start the Open Gears helper: \(message)"
        case .timedOut:
            return "The operation timed out and was stopped. Reconnect the adapter and refresh the bicycle before trying again."
        case .command(let message):
            return message.isEmpty ? "The helper did not complete the operation." : message
        }
    }
}

// Process execution and pipe reads always happen away from the main actor.
enum CLIRunner {
    static func run(
        executable: URL,
        arguments: [String],
        timeout: TimeInterval = 45,
        terminationGrace: TimeInterval = 25,
        allowFailure: Bool = false
    ) async throws -> CLIResult {
        try await Task.detached(priority: .userInitiated) {
            try runBlocking(
                executable: executable,
                arguments: arguments,
                timeout: timeout,
                terminationGrace: terminationGrace,
                allowFailure: allowFailure
            )
        }.value
    }

    private static func runBlocking(
        executable: URL,
        arguments: [String],
        timeout: TimeInterval,
        terminationGrace: TimeInterval,
        allowFailure: Bool
    ) throws -> CLIResult {
        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw CLIError.missingHelper(executable.path)
        }

        let process = Process()
        let outputPipe = Pipe()
        let errorPipe = Pipe()
        process.executableURL = executable
        process.arguments = arguments
        process.standardInput = FileHandle.nullDevice
        process.standardOutput = outputPipe
        process.standardError = errorPipe

        let exited = DispatchSemaphore(value: 0)
        process.terminationHandler = { _ in exited.signal() }
        do {
            try process.run()
        } catch {
            throw CLIError.launch(error.localizedDescription)
        }

        let state = ProcessState(process: process)
        let timeoutQueue = DispatchQueue(label: "com.opengears.app.timeout")
        let timer = DispatchSource.makeTimerSource(queue: timeoutQueue)
        timer.schedule(deadline: .now() + timeout)
        timer.setEventHandler {
            state.requestTermination()
            // An interrupted write may need a readback, service cleanup and
            // adapter reset before releasing USB. Allow the full cleanup budget.
            timeoutQueue.asyncAfter(deadline: .now() + terminationGrace) {
                state.forceTerminationIfNeeded()
            }
        }
        timer.resume()

        // Draining both pipes independently prevents full-pipe deadlocks.
        let collector = OutputCollector()
        let readers = DispatchGroup()
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            collector.storeOutput(outputPipe.fileHandleForReading.readDataToEndOfFile())
            readers.leave()
        }
        readers.enter()
        DispatchQueue.global(qos: .userInitiated).async {
            collector.storeError(errorPipe.fileHandleForReading.readDataToEndOfFile())
            readers.leave()
        }

        exited.wait()
        state.finish()
        timer.cancel()
        readers.wait()
        try? outputPipe.fileHandleForReading.close()
        try? errorPipe.fileHandleForReading.close()

        let (output, errorOutput) = collector.contents()
        let timedOut = state.didTimeOut
        if timedOut && (!allowFailure || process.terminationReason != .exit || output.isEmpty) {
            throw CLIError.timedOut
        }
        let diagnostic = String(decoding: errorOutput, as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        let succeeded = !timedOut && process.terminationReason == .exit && process.terminationStatus == 0
        guard succeeded || (allowFailure && process.terminationReason == .exit) else {
            throw CLIError.command(diagnostic)
        }
        return CLIResult(output: output, diagnostic: diagnostic, succeeded: succeeded, timedOut: timedOut)
    }
}

private final class ProcessState: @unchecked Sendable {
    private let lock = NSLock()
    private let process: Process
    private var finished = false
    private var timedOut = false

    init(process: Process) { self.process = process }

    var didTimeOut: Bool {
        lock.lock()
        defer { lock.unlock() }
        return timedOut
    }

    func requestTermination() {
        lock.lock()
        defer { lock.unlock() }
        guard !finished, process.isRunning else { return }
        timedOut = true
        process.terminate()
    }

    func forceTerminationIfNeeded() {
        lock.lock()
        defer { lock.unlock() }
        guard !finished, process.isRunning else { return }
        kill(process.processIdentifier, SIGKILL)
    }

    func finish() {
        lock.lock()
        finished = true
        lock.unlock()
    }
}

private final class OutputCollector: @unchecked Sendable {
    private let lock = NSLock()
    private var output = Data()
    private var error = Data()

    func storeOutput(_ data: Data) {
        lock.lock()
        output = data
        lock.unlock()
    }

    func storeError(_ data: Data) {
        lock.lock()
        error = data
        lock.unlock()
    }

    func contents() -> (Data, Data) {
        lock.lock()
        defer { lock.unlock() }
        return (output, error)
    }
}
