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

    func readAdapter() { read(kind: .adapter) }
    func readBicycle() { read(kind: .bicycle) }

    private func read(kind: ReportKind) {
        guard canRead, let device = selectedDevice else { return }
        var arguments = kind == .adapter ? ["adapter", "info"] : ["bike", "inspect"]
        arguments += ["--bus", String(device.bus), "--address", String(device.address)]
        if let supportFile {
            arguments += ["--firmware", supportFile.path]
        }
        begin(kind == .adapter ? "Reading the adapter…" : "Reading bicycle components and paddles…")
        reports.removeValue(forKey: kind)
        if kind == .adapter { adapterInfo = nil } else { bicycle = nil }
        Task {
            defer { finish() }
            do {
                let result = try await CLIRunner.run(executable: helperURL, arguments: arguments)
                if kind == .adapter {
                    adapterInfo = try decoder.decode(AdapterInfo.self, from: result.output)
                } else {
                    bicycle = try decoder.decode(BikeSnapshot.self, from: result.output)
                }
                reports[kind] = result.output
                notice = result.diagnostic.isEmpty ? nil : result.diagnostic
            } catch {
                errorMessage = error.localizedDescription
            }

            // Loading the support file can re-enumerate the same USB port at a
            // new address. Resolve the next selection from that physical path.
            activity = "Refreshing the USB connection…"
            do {
                try await refreshDevices(preserving: device)
            } catch {
                notice = "Check the USB connection again before starting another read."
            }
        }
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
    }

    private func finish() {
        isBusy = false
        AppLifecycle.shared.operationIsRunning = false
        activity = ""
    }

    private func clearReadReports() {
        adapterInfo = nil
        bicycle = nil
        reports.removeValue(forKey: .adapter)
        reports.removeValue(forKey: .bicycle)
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
