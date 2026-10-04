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
@MainActor
struct AppModelTests {
    static func main() async {
        do {
            let directory = FileManager.default.temporaryDirectory
                .appendingPathComponent("open-gears-model-tests-\(UUID().uuidString)")
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            defer { try? FileManager.default.removeItem(at: directory) }
            let helper = directory.appendingPathComponent("fake-helper")
            try Data(helperScript.utf8).write(to: helper)
            try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: helper.path)
            try Data(adapterJSON.utf8).write(to: directory.appendingPathComponent("adapter.json"))

            try setDevices(directory, bus: 0, address: 12, path: [1, 4])
            let model = AppModel(helperURL: helper)
            model.discover()
            try await waitUntilIdle(model)
            try expect(model.selectedDevice?.address == 12, "Initial adapter was not selected.")

            // A support-file load changes the USB address without changing the physical port.
            try setDevices(directory, bus: 0, address: 13, path: [1, 4])
            model.readAdapter()
            try await waitUntilIdle(model)
            try expect(model.selectedDevice?.address == 13, "The new USB address was not selected.")
            try expect(model.adapterInfo?.firmwareVersion == "3.0.1", "Same-port refresh erased adapter information.")
            try expect(model.reports[.adapter] != nil, "Same-port refresh erased the exportable adapter report.")
            try expect(!model.isBusy && !AppLifecycle.shared.operationIsRunning, "Adapter read did not release its busy state.")
            print("PASS same physical adapter retains reports after address change")

            // Adapter A's report must not appear alongside a replacement adapter B.
            try setDevices(directory, bus: 0, address: 24, path: [1, 9])
            try Data().write(to: directory.appendingPathComponent("bike-fails"))
            model.readBicycle()
            try await waitUntilIdle(model)
            try expect(model.selectedDevice?.path == [1, 9], "Replacement adapter was not selected.")
            try expect(model.adapterInfo == nil && model.bicycle == nil, "Replacement adapter retained another adapter's information.")
            try expect(model.reports[.adapter] == nil && model.reports[.bicycle] == nil, "Replacement adapter retained another adapter's exportable reports.")
            try expect(model.errorMessage == "The bicycle could not be read.", "Bicycle failure was not shown.")
            try expect(!model.isBusy && !AppLifecycle.shared.operationIsRunning, "Failed bicycle read did not release its busy state.")
            print("PASS failed bicycle read clears reports when physical adapter changes")

            model.readAdapter()
            try await waitUntilIdle(model)
            try expect(model.adapterInfo != nil && model.reports[.adapter] != nil, "Replacement adapter report was not populated.")
            try Data().write(to: directory.appendingPathComponent("devices-fail"))
            model.readBicycle()
            try await waitUntilIdle(model)
            try expect(model.devices.isEmpty && model.selectedID == nil, "Failed refresh retained a selected adapter.")
            try expect(model.adapterInfo == nil && model.bicycle == nil, "Failed refresh retained adapter or bicycle information.")
            try expect(model.reports[.adapter] == nil && model.reports[.bicycle] == nil, "Failed refresh retained adapter or bicycle reports.")
            try expect(model.notice == "Check the USB connection again before starting another read.", "Failed refresh recovery instruction was lost.")
            try expect(!model.isBusy && !AppLifecycle.shared.operationIsRunning, "Failed refresh did not release its busy state.")
            print("PASS failed refresh clears reports and releases busy state")
        } catch {
            FileHandle.standardError.write(Data("FAIL \(error.localizedDescription)\n".utf8))
            exit(1)
        }
    }

    private static func waitUntilIdle(_ model: AppModel) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(5))
        while model.isBusy {
            if ContinuousClock.now >= deadline {
                throw TestFailure(message: "Model operation did not finish within five seconds.")
            }
            try await Task.sleep(for: .milliseconds(10))
        }
    }

    private static func setDevices(_ directory: URL, bus: Int, address: Int, path: [Int]) throws {
        let device: [String: Any] = [
            "bus": bus, "address": address, "path": path,
            "vendor_id": "1e44", "product_id": "7220", "usb_version": "2.0",
            "device_version": "1.0", "speed": "full",
            "configurations": [["number": 2, "layout": "bidirectional-bulk"]],
        ]
        let json = try JSONSerialization.data(withJSONObject: [device], options: [.sortedKeys])
        try json.write(to: directory.appendingPathComponent("devices.json"))
    }

    private static let adapterJSON = #"{"link_reply_hex":"00","firmware_reply_hex":"300100","firmware_version":"3.0.1","note":"test adapter"}"#

    private static let helperScript = #"""
    #!/bin/sh
    set -eu
    fixture_dir=${0%/*}
    case "$1" in
        devices)
            if test -f "$fixture_dir/devices-fail"; then
                printf '%s\n' 'USB discovery failed.' >&2
                exit 1
            fi
            cat "$fixture_dir/devices.json"
            ;;
        adapter)
            cat "$fixture_dir/adapter.json"
            ;;
        bike)
            if test -f "$fixture_dir/bike-fails"; then
                printf '%s\n' 'The bicycle could not be read.' >&2
                exit 1
            fi
            exit 64
            ;;
        *) exit 64 ;;
    esac
    """#
}
