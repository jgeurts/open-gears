import AppKit
import Foundation
import SwiftUI
import UniformTypeIdentifiers

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var devices: [USBDevice] = []
    @Published private(set) var selectedID: String?
    @Published private(set) var supportFile: URL?
    @Published private(set) var adapterInfo: AdapterInfo?
    @Published private(set) var bicycle: BikeSnapshot?
    @Published private(set) var isBusy = false
    @Published private(set) var activity = ""
    @Published private(set) var errorMessage: String?
    @Published private(set) var notice: String?
    @Published private(set) var hasCheckedConnection = false
    @Published private(set) var reports: [ReportKind: Data] = [:]
    @Published private(set) var drafts: [Int: PaddleDraft] = [:]
    @Published private(set) var bicycleIsCurrent = false
    @Published private(set) var applyVerified = false
    @Published private(set) var applyUncertain = false
    @Published private(set) var lastApplyResult: PaddleApplyResult?
    @Published private(set) var lastOperationWasApply = false

    let helperURL: URL
    private let decoder = JSONDecoder()

    init(helperURL: URL? = nil) {
        if let helperURL {
            self.helperURL = helperURL
        } else if let developmentPath = ProcessInfo.processInfo.environment["OPEN_GEARS_CLI"], !developmentPath.isEmpty {
            self.helperURL = URL(fileURLWithPath: developmentPath)
        } else {
            self.helperURL = Bundle.main.bundleURL.appendingPathComponent("Contents/MacOS/open-gears")
        }
        if let savedPath = UserDefaults.standard.string(forKey: "controllerSupportFile"),
           FileManager.default.isReadableFile(atPath: savedPath) {
            supportFile = URL(fileURLWithPath: savedPath)
        }
    }

    var selectedDevice: USBDevice? { devices.first { $0.id == selectedID } }
    var availableReports: [ReportKind] { ReportKind.allCases.filter { reports[$0] != nil } }
    var canRead: Bool {
        guard let device = selectedDevice, !isBusy else { return false }
        return device.supportsRuntime || (device.needsSupportFile && supportFile != nil)
    }

    var connectionSummary: String {
        if bicycleIsCurrent, let bicycle { return "\(bicycle.units.count) components identified · SM-BCR2 attached" }
        if selectedDevice != nil { return "SM-BCR2 attached · bicycle connection not confirmed" }
        if devices.count > 1 { return "Choose one of the \(devices.count) attached adapters" }
        return hasCheckedConnection ? "No SM-BCR2 found" : "Checking for the SM-BCR2…"
    }

    var applyRecoveryExplanation: String {
        if let result = lastApplyResult, !result.changes.isEmpty,
           result.changes.allSatisfy({ $0.status == "verified" }) {
            return "Every requested assignment was verified, but connection cleanup failed. Reconnect and refresh before making more changes."
        }
        if lastApplyResult?.changes.contains(where: { $0.status == "verified" }) == true {
            return "Some assignments were verified. Refresh to read all current assignments before making more changes."
        }
        return "A write may have reached the bicycle. Refresh to read its actual assignments before trying again."
    }

    var friendlyErrorMessage: String? {
        guard let errorMessage else { return nil }
        if errorMessage.contains("no bicycle components detected") {
            return "The adapter is attached, but no bicycle components responded. Check the bicycle-side plug and charge the bicycle battery before connecting again."
        }
        if errorMessage.contains("libusb: pipe error") {
            return "The adapter rejected a connection request. Unplug its USB cable, reconnect it, and connect the bicycle again."
        }
        return errorMessage.hasPrefix("open-gears: ") ? String(errorMessage.dropFirst(12)) : errorMessage
    }

    var pendingChanges: [PendingPaddleChange] {
        (bicycle?.units ?? []).flatMap { unit -> [PendingPaddleChange] in
            guard let original = unit.paddles, let draft = drafts[unit.slot] else { return [] }
            var changes: [PendingPaddleChange] = []
            if draft.a != original.a {
                changes.append(PendingPaddleChange(slot: unit.slot, model: unit.model, key: .x, before: original.a, after: draft.a))
            }
            if draft.b != original.b {
                changes.append(PendingPaddleChange(slot: unit.slot, model: unit.model, key: .y, before: original.b, after: draft.b))
            }
            return changes
        }
    }

    var hasPendingChanges: Bool { !pendingChanges.isEmpty }
    var canApply: Bool {
        guard canRead, bicycleIsCurrent, hasPendingChanges,
              let identity = bicycle?.adapter, let selectedDevice,
              identity.bus == selectedDevice.bus, identity.path == selectedDevice.path else { return false }
        return (bicycle?.units ?? []).filter { drafts[$0.slot] != nil }.allSatisfy { unit in
            guard let draft = drafts[unit.slot], let original = unit.paddles else { return false }
            return unit.canEditPaddles && unit.firmwareVersion != nil &&
                (draft.a == original.a || PaddleFunction.standard.contains(draft.a)) &&
                (draft.b == original.b || PaddleFunction.standard.contains(draft.b))
        }
    }

    func paddleValue(slot: Int, key: PaddleKey) -> Int? {
        guard let unit = bicycle?.units.first(where: { $0.slot == slot }), let paddles = unit.paddles else { return nil }
        let draft = drafts[slot] ?? PaddleDraft(a: paddles.a, b: paddles.b)
        return key == .x ? draft.a : draft.b
    }

    func setPaddle(slot: Int, key: PaddleKey, value: Int) {
        guard !isBusy, bicycleIsCurrent,
              let unit = bicycle?.units.first(where: { $0.slot == slot }), unit.canEditPaddles,
              let paddles = unit.paddles,
              PaddleFunction.standard.contains(value) || value == (key == .x ? paddles.a : paddles.b) else { return }
        var draft = drafts[slot] ?? PaddleDraft(a: paddles.a, b: paddles.b)
        if key == .x { draft.a = value } else { draft.b = value }
        if draft.a == paddles.a && draft.b == paddles.b { drafts.removeValue(forKey: slot) }
        else { drafts[slot] = draft }
        applyVerified = false
        notice = nil
    }

    func discardPaddleChanges() {
        guard !isBusy else { return }
        drafts = [:]
    }

    func selectDevice(_ id: String) {
        guard !isBusy, id != selectedID else { return }
        selectedID = id
        clearReadReports()
        errorMessage = nil
        notice = nil
    }

    func chooseSupportFile() {
        guard !isBusy else { return }
        let panel = NSOpenPanel()
        panel.title = "Choose the Shimano adapter support file"
        panel.message = "Select umpf3410.i51 from your Shimano SM-BCR2 Windows driver. Open Gears verifies the file before using it."
        panel.prompt = "Choose file"
        panel.canChooseDirectories = false
        panel.canChooseFiles = true
        panel.allowsMultipleSelection = false
        guard panel.runModal() == .OK, let url = panel.url else { return }
        supportFile = url
        UserDefaults.standard.set(url.path, forKey: "controllerSupportFile")
        errorMessage = nil
        notice = nil
    }

    func forgetSupportFile() {
        guard !isBusy else { return }
        supportFile = nil
        UserDefaults.standard.removeObject(forKey: "controllerSupportFile")
    }

    func dismissError() { errorMessage = nil }

    func discover() {
        guard !isBusy else { return }
        begin("Checking the USB connection…")
        let previous = selectedDevice
        Task {
            defer { finish() }
            do {
                try await refreshDevices(preserving: previous)
                notice = devices.isEmpty ? "Connect your SM-BCR2 with a USB data cable, then check again." : nil
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func connectBicycle() {
        guard !isBusy else { return }
        begin("Finding the bicycle connection…")
        bicycleIsCurrent = false
        let previous = selectedDevice
        Task {
            defer { finish() }
            do {
                try await refreshDevices(preserving: previous)
                guard let device = selectedDevice else {
                    notice = devices.isEmpty ? "Connect the SM-BCR2 to your Mac with a USB data cable, then connect again." : "Choose an adapter in Settings, then connect again."
                    return
                }
                guard device.supportsRuntime || (device.needsSupportFile && supportFile != nil) else {
                    notice = "Choose the adapter support file in Settings (gear), then connect again."
                    return
                }
                activity = "Reading the adapter…"
                try await performRead(kind: .adapter, device: device)
                try await refreshDevices(preserving: device)
                guard let currentDevice = selectedDevice, currentDevice.bus == device.bus, currentDevice.path == device.path else {
                    throw CLIError.command("The adapter disconnected. Reconnect it, then connect the bicycle again.")
                }
                activity = "Reading bicycle components and paddle assignments…"
                try await performRead(kind: .bicycle, device: currentDevice)
                try await refreshDevices(preserving: currentDevice)
            } catch {
                bicycleIsCurrent = false
                errorMessage = error.localizedDescription
            }
        }
    }

    func readAdapter() { read(kind: .adapter) }
    func readBicycle() { read(kind: .bicycle) }

    private func read(kind: ReportKind) {
        guard canRead, let device = selectedDevice else { return }
        begin(kind == .adapter ? "Reading the adapter…" : "Reading bicycle components and paddles…")
        if kind == .bicycle { bicycleIsCurrent = false }
        Task {
            defer { finish() }
            do {
                try await performRead(kind: kind, device: device)
            } catch {
                errorMessage = error.localizedDescription
            }
            activity = "Refreshing the USB connection…"
            do {
                try await refreshDevices(preserving: device)
            } catch {
                notice = "Check the USB connection again before starting another read."
            }
        }
    }

    private func connectionArguments(device: USBDevice) -> [String] {
        var arguments = ["--bus", String(device.bus), "--address", String(device.address)]
        if let supportFile { arguments += ["--firmware", supportFile.path] }
        return arguments
    }

    private func performRead(kind: ReportKind, device: USBDevice) async throws {
        let command = kind == .adapter ? ["adapter", "info"] : ["bike", "inspect"]
        let result = try await CLIRunner.run(executable: helperURL, arguments: command + connectionArguments(device: device))
        if kind == .adapter {
            adapterInfo = try decoder.decode(AdapterInfo.self, from: result.output)
        } else {
            acceptSnapshot(try decoder.decode(BikeSnapshot.self, from: result.output), retainingDrafts: true)
        }
        reports[kind] = result.output
        notice = result.diagnostic.isEmpty ? nil : result.diagnostic
    }

    private func acceptSnapshot(_ snapshot: BikeSnapshot, retainingDrafts: Bool) {
        let old = bicycle
        var retained: [Int: PaddleDraft] = [:]
        if retainingDrafts, old?.adapter?.bus == snapshot.adapter?.bus,
           old?.adapter?.path == snapshot.adapter?.path {
            for unit in snapshot.units {
                guard let draft = drafts[unit.slot], let original = old?.units.first(where: { $0.slot == unit.slot }),
                      unit.canEditPaddles, unit.series == original.series, unit.number == original.number,
                      unit.part == original.part, unit.firmwareVersion == original.firmwareVersion,
                      unit.paddles?.raw.prefix(4) == original.paddles?.raw.prefix(4) else { continue }
                retained[unit.slot] = draft
            }
        }
        bicycle = snapshot
        drafts = retained
        bicycleIsCurrent = true
        applyUncertain = false
    }

    func applyPaddleChanges() {
        guard canApply, let snapshot = bicycle, let identity = snapshot.adapter, let device = selectedDevice else { return }
        let changes = snapshot.units.compactMap { unit -> PaddlePlan.Change? in
            guard let draft = drafts[unit.slot], let paddles = unit.paddles,
                  let firmware = unit.firmwareVersion else { return nil }
            return PaddlePlan.Change(slot: unit.slot, series: unit.series, number: unit.number, part: unit.part,
                                     firmwareVersion: firmware, beforeRaw: paddles.raw, a: draft.a, b: draft.b)
        }
        guard changes.count == drafts.count else {
            errorMessage = "Refresh the bicycle to read each shifter's firmware before applying changes."
            return
        }
        let plan = PaddlePlan(adapter: .init(bus: identity.bus, path: identity.path), changes: changes)
        begin("Applying paddle changes and verifying the bicycle…")
        lastOperationWasApply = true
        applyVerified = false
        lastApplyResult = nil
        Task {
            defer { finish() }
            let directory = FileManager.default.temporaryDirectory.appendingPathComponent("open-gears-plan-\(UUID().uuidString)")
            defer { try? FileManager.default.removeItem(at: directory) }
            var helperWasInvoked = false
            do {
                try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false,
                                                        attributes: [.posixPermissions: 0o700])
                let planURL = directory.appendingPathComponent("paddles.json")
                let data = try JSONEncoder().encode(plan)
                try data.write(to: planURL, options: .atomic)
                try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: planURL.path)
                var arguments = ["bike", "paddles", "apply", "--plan", planURL.path, "--bus", String(device.bus)]
                if let supportFile { arguments += ["--firmware", supportFile.path] }
                helperWasInvoked = true
                let response = try await CLIRunner.run(executable: helperURL, arguments: arguments, timeout: 120, allowFailure: true)
                let result: PaddleApplyResult
                do {
                    result = try decoder.decode(PaddleApplyResult.self, from: response.output)
                } catch {
                    throw CLIError.command(response.diagnostic.isEmpty ? "The helper returned no verifiable apply result." : response.diagnostic)
                }
                reports[.paddles] = response.output
                lastApplyResult = result
                switch result.status {
                case "verified":
                    guard let actual = result.snapshot else {
                        throw CLIError.command("The helper reported verification without returning the bicycle's assignments.")
                    }
                    acceptSnapshot(actual, retainingDrafts: false)
                    if let object = try JSONSerialization.jsonObject(with: response.output) as? [String: Any],
                       let snapshotObject = object["snapshot"] {
                        reports[.bicycle] = try JSONSerialization.data(withJSONObject: snapshotObject,
                                                                      options: [.prettyPrinted, .sortedKeys])
                    }
                    applyVerified = true
                    let hadReplyWarning = result.changes.contains { !($0.warning ?? "").isEmpty }
                    notice = hadReplyWarning ? "Paddle changes applied and verified. An adapter reply was missed; the assignments were confirmed by reading them back." : "Paddle changes applied and verified on the bicycle."
                    if !response.succeeded && !response.diagnostic.isEmpty { errorMessage = response.diagnostic }
                case "unchanged":
                    if let actual = result.snapshot { acceptSnapshot(actual, retainingDrafts: true) }
                    let readbackMatchesBefore = !result.changes.isEmpty && result.changes.allSatisfy { change in
                        guard let actual = change.actualRaw else { return false }
                        return actual.prefix(4) == change.beforeRaw.prefix(4)
                    }
                    notice = readbackMatchesBefore ? "Assignments unchanged. The latest readback did not show your requested change." : "Paddle changes were not completed."
                    errorMessage = applyDiagnostic(result, fallback: response.diagnostic)
                default:
                    bicycleIsCurrent = false
                    applyUncertain = true
                    errorMessage = applyDiagnostic(result, fallback: response.diagnostic) ?? "The apply operation could not be verified."
                }
            } catch {
                var couldHaveWritten = helperWasInvoked
                if let cliError = error as? CLIError {
                    switch cliError {
                    case .missingHelper, .launch: couldHaveWritten = false
                    default: break
                    }
                }
                if couldHaveWritten { bicycleIsCurrent = false }
                applyUncertain = couldHaveWritten
                errorMessage = error.localizedDescription
            }
            activity = "Refreshing the USB connection…"
            do {
                try await refreshDevices(preserving: device)
            } catch {
                notice = "Reconnect the adapter and refresh the bicycle before making another change."
            }
        }
    }

    private func applyDiagnostic(_ result: PaddleApplyResult, fallback: String) -> String? {
        if let error = result.error, !error.isEmpty { return error }
        let errors = result.changes.compactMap(\.error).filter { !$0.isEmpty }
        if !errors.isEmpty { return errors.joined(separator: "\n") }
        return fallback.isEmpty ? nil : fallback
    }

    private func refreshDevices(preserving previous: USBDevice?) async throws {
        do {
            let result = try await CLIRunner.run(executable: helperURL, arguments: ["devices", "--json"])
            let found = try decoder.decode([USBDevice].self, from: result.output)
            devices = found
            hasCheckedConnection = true
            let matching = found.first { $0.bus == previous?.bus && $0.path == previous?.path }
            selectedID = matching?.id ?? (found.count == 1 ? found.first?.id : nil)
            if previous?.bus != selectedDevice?.bus || previous?.path != selectedDevice?.path {
                clearReadReports()
            }
            reports[.usb] = result.output
        } catch {
            devices = []
            selectedID = nil
            hasCheckedConnection = true
            clearReadReports()
            throw error
        }
    }

    func export(_ kind: ReportKind) {
        guard !isBusy, let data = reports[kind] else { return }
        let panel = NSSavePanel()
        panel.title = "Export \(kind.rawValue.lowercased())"
        panel.nameFieldStringValue = kind.filename
        panel.allowedContentTypes = [.json]
        panel.canCreateDirectories = true
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do {
            try data.write(to: url, options: .atomic)
            notice = "Saved \(url.lastPathComponent)."
        } catch {
            errorMessage = "Could not save the report: \(error.localizedDescription)"
        }
    }

    private func begin(_ description: String) {
        isBusy = true
        AppLifecycle.shared.operationIsRunning = true
        activity = description
        errorMessage = nil
        notice = nil
        applyVerified = false
        lastOperationWasApply = false
    }

    private func finish() {
        isBusy = false
        AppLifecycle.shared.operationIsRunning = false
        activity = ""
    }

    private func clearReadReports() {
        adapterInfo = nil
        bicycle = nil
        drafts = [:]
        bicycleIsCurrent = false
        applyVerified = false
        applyUncertain = false
        lastApplyResult = nil
        reports.removeValue(forKey: .adapter)
        reports.removeValue(forKey: .bicycle)
        reports.removeValue(forKey: .paddles)
    }
}

@MainActor
final class AppLifecycle {
    static let shared = AppLifecycle()
    var operationIsRunning = false
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard AppLifecycle.shared.operationIsRunning else { return .terminateNow }
        let alert = NSAlert()
        alert.messageText = "A connection operation is still running"
        alert.informativeText = "Wait for it to finish or time out before quitting so the adapter can close its connection."
        alert.addButton(withTitle: "Keep Open Gears open")
        alert.runModal()
        return .terminateCancel
    }
}
