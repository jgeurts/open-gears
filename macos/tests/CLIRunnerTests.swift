import Darwin
import Foundation

private struct TestFailure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

private func expect(_ condition: Bool, _ message: String) throws {
    if !condition { throw TestFailure(message: message) }
}

@main
struct CLIRunnerTests {
    static func main() async {
        do {
            guard CommandLine.arguments.count == 2 else {
                throw TestFailure(message: "Pass the process fixture executable.")
            }
            let helper = URL(fileURLWithPath: CommandLine.arguments[1])
            let directory = FileManager.default.temporaryDirectory
                .appendingPathComponent("open-gears-process-tests-\(UUID().uuidString)")
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            defer { try? FileManager.default.removeItem(at: directory) }

            let mutationMarker = directory.appendingPathComponent("argument-was-evaluated")
            let literalArguments = ["space separated", "$(touch \(mutationMarker.path))", "semi;colon", "", "🚲"]
            let success = try await CLIRunner.run(
                executable: helper, arguments: ["arguments"] + literalArguments, timeout: 5
            )
            let returnedArguments = try JSONDecoder().decode([String].self, from: success.output)
            try expect(returnedArguments == literalArguments, "Arguments were not passed literally.")
            try expect(!FileManager.default.fileExists(atPath: mutationMarker.path), "An argument was evaluated by a shell.")
            try expect(success.diagnostic == "helper diagnostic", "Successful diagnostic was lost.")
            print("PASS literal arguments and successful output")

            let large = try await CLIRunner.run(executable: helper, arguments: ["large-output"], timeout: 5)
            try expect(large.output == Data(repeating: 0x6f, count: 1_048_576), "Large stdout was truncated.")
            try expect(large.diagnostic == String(repeating: "e", count: 1_048_576), "Large stderr was truncated.")
            print("PASS concurrent stdout and stderr above pipe capacity")

            do {
                _ = try await CLIRunner.run(executable: helper, arguments: ["failure"], timeout: 5)
                throw TestFailure(message: "A failed command was accepted.")
            } catch CLIError.command(let diagnostic) {
                try expect(diagnostic == "The adapter was disconnected.", "Failure diagnostic was lost.")
            }
            print("PASS failed command diagnostic")

            let missing = directory.appendingPathComponent("missing-helper")
            do {
                _ = try await CLIRunner.run(executable: missing, arguments: [])
                throw TestFailure(message: "A missing helper was accepted.")
            } catch CLIError.missingHelper(let path) {
                try expect(path == missing.path, "Missing-helper path was lost.")
            }
            print("PASS missing helper")

            let gracefulMarker = directory.appendingPathComponent("graceful")
            let gracefulStart = ContinuousClock.now
            try await expectTimeout(helper, arguments: ["graceful-timeout", gracefulMarker.path])
            try expect(gracefulStart.duration(to: .now) < .seconds(4), "Graceful timeout did not finish promptly.")
            try expect(try String(contentsOf: gracefulMarker, encoding: .utf8) == "terminated", "SIGTERM cleanup did not run.")
            print("PASS timeout allows graceful SIGTERM cleanup")

            let ignoredMarker = directory.appendingPathComponent("ignored")
            let forcedStart = ContinuousClock.now
            try await expectTimeout(helper, arguments: ["ignored-timeout", ignoredMarker.path])
            try expect(forcedStart.duration(to: .now) < .seconds(4), "Ignored SIGTERM was not bounded.")
            guard let pid = Int32(try String(contentsOf: ignoredMarker, encoding: .utf8)) else {
                throw TestFailure(message: "Ignored-SIGTERM helper did not start.")
            }
            try expect(kill(pid, 0) == -1 && errno == ESRCH, "Timed-out helper is still alive.")
            print("PASS timeout forcibly stops a helper that ignores SIGTERM")
        } catch {
            FileHandle.standardError.write(Data("FAIL \(error.localizedDescription)\n".utf8))
            exit(1)
        }
    }

    private static func expectTimeout(_ helper: URL, arguments: [String]) async throws {
        do {
            _ = try await CLIRunner.run(
                executable: helper, arguments: arguments, timeout: 0.75, terminationGrace: 0.5
            )
            throw TestFailure(message: "A timed-out command was accepted.")
        } catch CLIError.timedOut {
            // Both graceful exits and forced termination must remain reported as timeouts.
        }
    }
}
