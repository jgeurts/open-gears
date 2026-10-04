import Darwin
import Foundation

nonisolated(unsafe) private var terminationMarker: Int32 = -1

@main
struct ProcessFixture {
    static func main() throws {
        let arguments = Array(CommandLine.arguments.dropFirst())
        switch arguments.first {
        case "arguments":
            FileHandle.standardOutput.write(try JSONEncoder().encode(Array(arguments.dropFirst())))
            FileHandle.standardError.write(Data("  helper diagnostic\n".utf8))
        case "large-output":
            let finished = DispatchGroup()
            finished.enter()
            DispatchQueue.global().async {
                FileHandle.standardError.write(Data(repeating: 0x65, count: 1_048_576))
                finished.leave()
            }
            FileHandle.standardOutput.write(Data(repeating: 0x6f, count: 1_048_576))
            finished.wait()
        case "failure":
            FileHandle.standardError.write(Data("  The adapter was disconnected.\n".utf8))
            exit(23)
        case "graceful-timeout":
            let marker = URL(fileURLWithPath: arguments[1])
            terminationMarker = open(marker.path, O_WRONLY | O_CREAT | O_TRUNC, 0o600)
            guard terminationMarker >= 0 else { exit(65) }
            signal(SIGTERM) { _ in
                _ = lseek(terminationMarker, 0, SEEK_SET)
                _ = write(terminationMarker, "terminated", 10)
                _exit(0)
            }
            _ = write(terminationMarker, "ready", 5)
            while true { pause() }
        case "ignored-timeout":
            signal(SIGTERM, SIG_IGN)
            try Data(String(getpid()).utf8).write(to: URL(fileURLWithPath: arguments[1]))
            while true { pause() }
        default:
            exit(64)
        }
    }
}
