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

            try FileManager.default.removeItem(at: directory.appendingPathComponent("bike-fails"))
            try FileManager.default.removeItem(at: directory.appendingPathComponent("devices-fail"))
            try setDevices(directory, bus: 0, address: 25, path: [1, 9])
            try setBicycle(directory, bus: 0, address: 25, path: [1, 9])
            model.connectBicycle()
            try await waitUntilIdle(model)
            try expect(model.adapterInfo != nil && model.bicycleIsCurrent, "Connect did not read both adapter and bicycle.")
            try expect(model.bicycle?.units.first?.canEditPaddles == true, "Backend edit capability was lost.")
            model.setPaddle(slot: 1, key: .x, value: 2)
            try expect(model.pendingChanges.count == 1 && model.canApply, "A supported paddle change could not be staged.")
            model.setPaddle(slot: 2, key: .x, value: 3)
            try expect(model.drafts[2] == nil, "An unsupported shifter accepted a draft.")
            print("PASS single connection action and backend editing gates")

            try setDevices(directory, bus: 0, address: 26, path: [1, 9])
            model.discover()
            try await waitUntilIdle(model)
            try expect(model.drafts[1]?.a == 2 && model.canApply, "Same-port address change discarded a draft.")
            try setBicycle(directory, bus: 0, address: 26, path: [1, 9], raw: "010cffff")
            model.readBicycle()
            try await waitUntilIdle(model)
            try expect(model.drafts[1]?.a == 2 && model.canApply, "Fresh read discarded a draft when only ignored bytes changed.")
            print("PASS staged changes survive same bicycle reads and USB address changes")

            try writeApplyResult(directory, status: "unknown", exitFailure: true)
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(model.applyUncertain && !model.bicycleIsCurrent && !model.canApply,
                       "Unknown apply outcome allowed a second write.")
            try expect(model.drafts[1]?.a == 2, "Unknown apply outcome discarded the requested change.")
            try expect(model.errorMessage == "Readback failed.", "Structured failure's diagnostic was lost.")
            let planURL = directory.appendingPathComponent("captured-plan.json")
            let plan = try JSONSerialization.jsonObject(with: Data(contentsOf: planURL)) as! [String: Any]
            let changes = plan["changes"] as! [[String: Any]]
            let physicalAdapter = plan["adapter"] as! [String: Any]
            try expect(plan["version"] as? Int == 1 && physicalAdapter["path"] as? [Int] == [1, 9], "Plan lost physical adapter identity.")
            try expect(changes.first?["a"] as? Int == 2 && changes.first?["b"] as? Int == 1,
                       "Plan did not preserve the untouched paddle assignment.")
            try expect(changes.first?["before_raw"] as? String == "010cffff" && changes.first?["firmware_version"] as? String == "3.0.0 (revision 0)",
                       "Plan lost expected readback or firmware identity.")
            let privateMode = try String(contentsOf: directory.appendingPathComponent("plan-mode"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
            try expect(privateMode == "600", "Apply plan was not private.")
            print("PASS unknown apply preserves the draft and requires a fresh read")

            model.readBicycle()
            try await waitUntilIdle(model)
            try expect(model.canApply && !model.applyUncertain, "Fresh read did not recover an unknown outcome.")
            try writeApplyResult(directory, status: "unchanged", exitFailure: true)
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(!model.applyUncertain && !model.applyVerified && model.drafts[1]?.a == 2,
                       "Confirmed unchanged outcome was confused with an unknown or verified write.")
            try expect(model.notice == "Paddle changes were not completed.", "Confirmed unchanged result was not explained.")
            print("PASS confirmed unchanged result retains the requested change")
            try writeApplyResult(directory, status: "unchanged", exitFailure: true, actualRaw: "010c0000")
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(model.notice == "Assignments unchanged. The latest readback did not show your requested change.",
                       "Observed unchanged assignments were not distinguished from preflight failure.")
            print("PASS observed unchanged readback is distinguished from preflight failure")

            try writeApplyResult(directory, status: "partial", exitFailure: true, snapshot: directory.appendingPathComponent("bike.json"))
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(model.applyUncertain && !model.canApply && model.drafts[1]?.a == 2,
                       "Partial outcome allowed another write or discarded the draft.")
            try expect(model.reports[.paddles] != nil, "Partial apply result was unavailable for diagnosis.")
            print("PASS partial apply retains its report and blocks retry until refresh")
            model.readBicycle()
            try await waitUntilIdle(model)
            try writeApplyResult(directory, status: "partial", exitFailure: true, changeStatus: "verified", error: "Connection cleanup failed.")
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(model.applyRecoveryExplanation.hasPrefix("Every requested assignment was verified"),
                       "Verified assignments with failed cleanup were described as unverified.")
            try expect(model.errorMessage == "Connection cleanup failed." && !model.canApply,
                       "Top-level cleanup failure was hidden or allowed another write.")
            print("PASS verified readbacks with failed cleanup still require refresh")
            model.readBicycle()
            try await waitUntilIdle(model)
            try setBicycle(directory, bus: 0, address: 26, path: [1, 9], a: 2, raw: "210c0000")
            try writeApplyResult(directory, status: "verified", snapshot: directory.appendingPathComponent("bike.json"), warning: "Adapter acknowledgment was missed.")
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(model.applyVerified && !model.hasPendingChanges && model.bicycleIsCurrent,
                       "Verified apply did not update the displayed assignments and clear the draft.")
            try expect(model.bicycle?.units.first?.paddles?.a == 2 && model.reports[.bicycle] != nil,
                       "Verified readback was not available for display and export.")
            try expect(model.notice?.contains("confirmed by reading them back") == true, "A verified readback warning was hidden or treated as failure.")
            try expect(!model.isBusy && !AppLifecycle.shared.operationIsRunning, "Verified apply did not release busy state.")
            print("PASS verified apply displays actual assignments and clears pending changes")

            try setBicycle(directory, bus: 0, address: 26, path: [1, 9], a: 2, b: 15, raw: "2f0c0000")
            model.readBicycle()
            try await waitUntilIdle(model)
            model.setPaddle(slot: 1, key: .x, value: 3)
            try expect(model.canApply && model.drafts[1]?.b == 15, "Editing X did not preserve model-specific Y.")
            model.setPaddle(slot: 1, key: .y, value: 0)
            model.setPaddle(slot: 1, key: .y, value: 15)
            try expect(model.drafts[1]?.b == 15 && model.pendingChanges.count == 1,
                       "A paddle could not revert to its original model-specific assignment.")
            try setBicycle(directory, bus: 0, address: 26, path: [1, 9], a: 0, b: 15, raw: "0f0c0000")
            model.readBicycle()
            try await waitUntilIdle(model)
            try expect(!model.hasPendingChanges, "A changed baseline retained a stale draft.")
            print("PASS unchanged model-specific assignments are preserved and stale drafts invalidated")

            model.setPaddle(slot: 1, key: .x, value: 3)
            try Data("invalid result".utf8).write(to: directory.appendingPathComponent("apply.json"))
            try Data().write(to: directory.appendingPathComponent("apply-fails"))
            model.applyPaddleChanges()
            try await waitUntilIdle(model)
            try expect(model.applyUncertain && !model.canApply && model.drafts[1]?.a == 3,
                       "A missing structured result was treated as a safe retry.")
            try expect(model.errorMessage == "Readback failed.", "Unstructured apply failure hid the helper diagnostic.")
            print("PASS missing apply result conservatively requires refresh")

            try setDevices(directory, bus: 0, address: 27, path: [1, 10])
            model.discover()
            try await waitUntilIdle(model)
            try expect(model.bicycle == nil && !model.hasPendingChanges && !model.canApply,
                       "Replacement adapter retained a write plan for another bicycle.")
            print("PASS replacement adapter invalidates pending bicycle changes")
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

    private static func setBicycle(_ directory: URL, bus: Int, address: Int, path: [Int], a: Int = 0, b: Int = 1, raw: String = "010c0000") throws {
        let unit: [String: Any] = [
            "slot": 1, "series": 5, "number": 1, "part": 1, "part_known": true,
            "model": "ST-R785-L", "firmware_version": "3.0.0 (revision 0)", "paddle_edit_supported": true,
            "paddle_edit_reason": "", "paddles": ["a": a, "b": b, "c": 0, "s": 12, "raw": raw, "labels": [:]],
        ]
        let unsupported: [String: Any] = [
            "slot": 2, "series": 99, "number": 1, "part": 1, "part_known": true,
            "model": "Unknown shifter", "firmware_version": "1.0.0", "paddle_edit_supported": false,
            "paddle_edit_reason": "This shifter is not supported.", "paddles": ["a": 0, "b": 1, "raw": "01000000", "labels": [:]],
        ]
        let snapshot: [String: Any] = [
            "captured_at": "2026-10-04T12:00:00Z", "pc_slot": 30, "slot_bitmap": "0300",
            "units": [unit, unsupported], "note": "Test fixture", "adapter": ["bus": bus, "address": address, "path": path],
        ]
        try JSONSerialization.data(withJSONObject: snapshot, options: [.sortedKeys]).write(to: directory.appendingPathComponent("bike.json"))
    }

    private static func writeApplyResult(_ directory: URL, status: String, exitFailure: Bool = false, snapshot: URL? = nil,
                                         changeStatus: String? = nil, actualRaw: String? = nil, error: String? = nil, warning: String? = nil) throws {
        var change: [String: Any] = ["slot": 1, "status": changeStatus ?? status, "before_raw": "010cffff",
                                     "requested_raw": "210c0000", "error": status == "verified" ? "" : "Readback failed."]
        if let actualRaw { change["actual_raw"] = actualRaw }
        if let warning { change["warning"] = warning }
        var result: [String: Any] = ["status": status, "changes": [change]]
        if let error { result["error"] = error }
        if let snapshot { result["snapshot"] = try JSONSerialization.jsonObject(with: Data(contentsOf: snapshot)) }
        try JSONSerialization.data(withJSONObject: result, options: [.sortedKeys]).write(to: directory.appendingPathComponent("apply.json"))
        let failureMarker = directory.appendingPathComponent("apply-fails")
        if exitFailure { try Data().write(to: failureMarker) }
        else { try? FileManager.default.removeItem(at: failureMarker) }
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
            if test "$2" = paddles; then
                cp "$5" "$fixture_dir/captured-plan.json"
                stat -f '%Lp' "$5" > "$fixture_dir/plan-mode"
                cat "$fixture_dir/apply.json"
                if test -f "$fixture_dir/apply-fails"; then
                    printf '%s\n' 'Readback failed.' >&2
                    exit 1
                fi
                exit 0
            fi
            if test -f "$fixture_dir/bike-fails"; then
                printf '%s\n' 'The bicycle could not be read.' >&2
                exit 1
            fi
            cat "$fixture_dir/bike.json"
            ;;
        *) exit 64 ;;
    esac
    """#
}
