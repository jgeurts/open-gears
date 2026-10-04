import SwiftUI

@main
struct OpenGearsApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("Open Gears") {
            ContentView(model: model)
                .frame(minWidth: 920, minHeight: 660)
        }
        .defaultSize(width: 1080, height: 780)
        .windowStyle(.hiddenTitleBar)
        .commands {
            CommandGroup(replacing: .newItem) { }
            CommandMenu("Connection") {
                Button("Check USB Connection") { model.discover() }
                    .keyboardShortcut("r", modifiers: .command)
                    .disabled(model.isBusy)
                Button("Choose Adapter Support File…") { model.chooseSupportFile() }
                    .disabled(model.isBusy)
                Divider()
                Button("Read Adapter Information") { model.readAdapter() }
                    .disabled(!model.canRead)
                Button("Read Bicycle — Experimental") { model.readBicycle() }
                    .disabled(!model.canRead)
            }
        }
    }
}

private enum Theme {
    static let accent = Color(red: 0.05, green: 0.54, blue: 0.47)
    static let sidebar = Color(red: 0.09, green: 0.14, blue: 0.16)
    static let sidebarMuted = Color(red: 0.64, green: 0.73, blue: 0.74)
    static let sidebarAccent = Color(red: 0.43, green: 0.86, blue: 0.73)
    static let card = Color(nsColor: .controlBackgroundColor)
    static let border = Color.primary.opacity(0.08)
    static let warning = Color(red: 0.62, green: 0.39, blue: 0.04)
}

struct ContentView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        HStack(spacing: 0) {
            sidebar
                .frame(width: 268)
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    header
                    if model.isBusy { activity }
                    if let error = model.errorMessage { errorCard(error) }
                    if let notice = model.notice {
                        Label(notice, systemImage: "info.circle")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                            .accessibilityLabel("Notice: \(notice)")
                    }
                    connectionCard
                    bicycleSection
                    footer
                }
                .padding(32)
                .frame(maxWidth: 1040, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .topLeading)
            }
            .background(Color(nsColor: .windowBackgroundColor))
        }
        .tint(Theme.accent)
        .task {
            if !model.hasCheckedConnection && !model.isBusy { model.discover() }
        }
    }

    private var sidebar: some View {
        VStack(alignment: .leading, spacing: 28) {
            HStack(spacing: 12) {
                Image(systemName: "bicycle")
                    .font(.system(size: 28, weight: .medium))
                    .foregroundStyle(Theme.sidebarAccent)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 3) {
                    Text("Open Gears")
                        .font(.system(size: 21, weight: .semibold, design: .rounded))
                    Text("YOUR DI2 CONNECTION")
                        .font(.system(size: 9, weight: .semibold))
                        .tracking(1.6)
                        .foregroundStyle(Theme.sidebarMuted)
                }
            }
            .padding(.top, 10)

            VStack(alignment: .leading, spacing: 14) {
                sidebarHeading("01", "USB connection")
                HStack(spacing: 8) {
                    Circle()
                        .fill(model.selectedDevice == nil ? Theme.sidebarMuted : Theme.sidebarAccent)
                        .frame(width: 7, height: 7)
                        .accessibilityHidden(true)
                    Text(connectionStatus)
                        .font(.system(size: 13, weight: .medium))
                        .accessibilityLabel("Adapter status: \(connectionStatus)")
                }
                if model.devices.count > 1 {
                    Picker("Adapter", selection: Binding(
                        get: { model.selectedID ?? "" },
                        set: { model.selectDevice($0) }
                    )) {
                        Text("Choose an adapter").tag("")
                        ForEach(model.devices) { device in
                            Text(device.connectionName).tag(device.id)
                        }
                    }
                    .labelsHidden()
                    .disabled(model.isBusy)
                    .accessibilityLabel("Choose the USB adapter")
                }
                Button(action: model.discover) {
                    Label("Check USB connection", systemImage: "arrow.clockwise")
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 3)
                }
                .buttonStyle(.bordered)
                .disabled(model.isBusy)
                .keyboardShortcut("r", modifiers: .command)
                .accessibilityHint("Find connected Shimano SM-BCR2 adapters without starting a bicycle session")
                Text("Use a USB data cable and connect directly to your Mac.")
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.sidebarMuted)
                    .fixedSize(horizontal: false, vertical: true)
            }

            VStack(alignment: .leading, spacing: 14) {
                sidebarHeading("02", "Adapter support file")
                if let file = model.supportFile {
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: "doc.badge.checkmark")
                            .foregroundStyle(Theme.sidebarAccent)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(file.lastPathComponent)
                                .font(.system(size: 12, weight: .medium))
                                .lineLimit(2)
                            Text("Verified by the helper when used")
                                .font(.system(size: 10))
                                .foregroundStyle(Theme.sidebarMuted)
                        }
                    }
                    .help(file.path)
                } else {
                    Text("Choose umpf3410.i51 from your Shimano Windows driver if the adapter needs it.")
                        .font(.system(size: 11))
                        .foregroundStyle(Theme.sidebarMuted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                HStack(spacing: 8) {
                    Button(model.supportFile == nil ? "Choose file…" : "Change file…", action: model.chooseSupportFile)
                        .buttonStyle(.bordered)
                        .disabled(model.isBusy)
                        .accessibilityLabel("Choose the Shimano umpf3410.i51 adapter support file")
                    if model.supportFile != nil {
                        Button(action: model.forgetSupportFile) {
                            Image(systemName: "xmark")
                        }
                        .buttonStyle(.borderless)
                        .foregroundStyle(Theme.sidebarMuted)
                        .disabled(model.isBusy)
                        .help("Forget this file")
                        .accessibilityLabel("Forget the selected adapter support file")
                    }
                }
                Text("Loaded temporarily into the USB adapter. This is separate from bicycle firmware.")
                    .font(.system(size: 10))
                    .foregroundStyle(Theme.sidebarMuted)
                    .fixedSize(horizontal: false, vertical: true)
            }

            Spacer(minLength: 18)

            VStack(alignment: .leading, spacing: 10) {
                Label("Read-only bicycle access", systemImage: "eye")
                    .font(.system(size: 11, weight: .medium))
                Text("Settings editing and bicycle firmware updates are not available in this version.")
                    .font(.system(size: 10))
                    .foregroundStyle(Theme.sidebarMuted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(24)
        .foregroundStyle(.white)
        .background(Theme.sidebar)
        .environment(\.colorScheme, .dark)
    }

    private func sidebarHeading(_ number: String, _ title: String) -> some View {
        HStack(spacing: 8) {
            Text(number)
                .font(.system(size: 10, weight: .medium, design: .monospaced))
                .foregroundStyle(Theme.sidebarAccent)
            Text(title)
                .font(.system(size: 12, weight: .semibold))
        }
    }

    private var connectionStatus: String {
        if let device = model.selectedDevice { return "SM-BCR2 connected · \(device.address)" }
        if model.devices.count > 1 { return "\(model.devices.count) adapters found" }
        return model.hasCheckedConnection ? "No adapter selected" : "Connection not checked"
    }

    private var header: some View {
        HStack(alignment: .top, spacing: 20) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Bicycle overview")
                    .font(.system(size: 30, weight: .semibold, design: .rounded))
                Text("Inspect your Shimano Di2 connection and controls.")
                    .font(.system(size: 13))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            Menu {
                ForEach(model.availableReports) { kind in
                    Button(kind.rawValue) { model.export(kind) }
                }
            } label: {
                Label("Export JSON", systemImage: "square.and.arrow.up")
            }
            .menuStyle(.borderlessButton)
            .fixedSize()
            .disabled(model.availableReports.isEmpty || model.isBusy)
            .accessibilityLabel("Export a captured connection or bicycle report as JSON")
            .padding(.top, 9)
        }
    }

    private var activity: some View {
        HStack(spacing: 12) {
            ProgressView().controlSize(.small)
            Text(model.activity).font(.callout)
            Spacer()
            Text("One operation at a time")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding(14)
        .background(Theme.accent.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Busy: \(model.activity)")
    }

    private func errorCard(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "exclamationmark.circle.fill")
                .foregroundStyle(.red)
                .font(.title3)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 7) {
                Text("The operation needs attention")
                    .font(.system(size: 13, weight: .semibold))
                Text(message)
                    .font(.system(size: 12))
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            Button(action: model.dismissError) { Image(systemName: "xmark") }
                .buttonStyle(.borderless)
                .foregroundStyle(.secondary)
                .accessibilityLabel("Dismiss the error message")
        }
        .padding(18)
        .background(Color.red.opacity(0.06), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(Color.red.opacity(0.17), lineWidth: 1))
    }

    private var connectionCard: some View {
        SectionCard {
            HStack(alignment: .top, spacing: 16) {
                Image(systemName: "cable.connector")
                    .font(.system(size: 25, weight: .regular))
                    .foregroundStyle(Theme.accent)
                    .frame(width: 48, height: 48)
                    .background(Theme.accent.opacity(0.08), in: RoundedRectangle(cornerRadius: 12))
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 5) {
                    Text("Shimano SM-BCR2")
                        .font(.system(size: 18, weight: .semibold))
                    Text(model.selectedDevice == nil ? "Connect the adapter to begin." : "USB charger and bicycle connection adapter")
                        .font(.system(size: 12))
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if model.selectedDevice != nil {
                    StatusPill(title: "Connected", symbol: "checkmark.circle", color: Theme.accent)
                }
            }

            if let device = model.selectedDevice {
                Divider().padding(.vertical, 5)
                HStack(alignment: .top, spacing: 24) {
                    Metric(title: "USB connection", value: "Bus \(device.bus) · address \(device.address)")
                    Spacer(minLength: 0)
                    Metric(title: "USB speed", value: device.speed)
                    Spacer(minLength: 0)
                    Metric(title: "Adapter firmware", value: model.adapterInfo?.firmwareVersion ?? "Not read yet")
                }
                if device.isBehindHub {
                    Label("A USB hub is in the connection path. Shimano recommends a direct connection.", systemImage: "exclamationmark.triangle")
                        .font(.caption)
                        .foregroundStyle(Theme.warning)
                        .fixedSize(horizontal: false, vertical: true)
                }
                if device.needsSupportFile && model.supportFile == nil {
                    Text("Choose the adapter support file in the sidebar before reading.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                } else if !device.supportsRuntime && !device.needsSupportFile {
                    Text("This USB layout is not supported yet. Export the USB connection report for investigation.")
                        .font(.callout)
                        .foregroundStyle(Theme.warning)
                }
                HStack {
                    Button(action: model.readAdapter) {
                        Label("Read adapter information", systemImage: "info.circle")
                    }
                    .buttonStyle(.bordered)
                    .disabled(!model.canRead)
                    Spacer()
                }
                if let info = model.adapterInfo {
                    DisclosureGroup("Adapter reply details") {
                        VStack(alignment: .leading, spacing: 8) {
                            DetailRow(title: "Link reply", value: info.linkReplyHex)
                            DetailRow(title: "Firmware reply", value: info.firmwareReplyHex)
                            Text(info.note).font(.caption).foregroundStyle(.secondary)
                        }
                        .padding(.top, 8)
                    }
                    .font(.caption)
                    .foregroundStyle(.secondary)
                }
            } else {
                Text("Connect the USB adapter and its bicycle cable, then use Check USB connection. No bicycle data has been read.")
                    .font(.system(size: 12))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }

    private var bicycleSection: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack {
                Text("Components & controls")
                    .font(.system(size: 19, weight: .semibold, design: .rounded))
                Spacer()
                Button(action: model.readBicycle) {
                    Label(model.bicycle == nil ? "Read bicycle" : "Read again", systemImage: "bicycle")
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(!model.canRead)
                .accessibilityLabel("Read bicycle components and paddle assignments, experimental")
            }
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: "exclamationmark.triangle")
                    .font(.callout)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 4) {
                    Text("Experimental · decoding is unverified")
                        .font(.system(size: 12, weight: .semibold))
                    Text("Check returned component names and paddle labels against your bicycle. Keep it stationary while reading.")
                        .font(.system(size: 11))
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .foregroundStyle(Theme.warning)
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.orange.opacity(0.07), in: RoundedRectangle(cornerRadius: 10))

            if let snapshot = model.bicycle {
                HStack(alignment: .top) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text("\(snapshot.units.count) components returned")
                            .font(.system(size: 12, weight: .medium))
                        if let date = snapshot.date {
                            Text("Captured \(date.formatted(date: .abbreviated, time: .standard))")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    Spacer()
                    if snapshot.readErrorCount > 0 {
                        StatusPill(title: "\(snapshot.readErrorCount) read issues", symbol: "exclamationmark.circle", color: Theme.warning)
                    }
                }
                ForEach(snapshot.units) { unit in ComponentCard(unit: unit) }
                if let raw = snapshot.batteryLevelRaw {
                    Label("Battery reading: \(raw) (raw value; not a percentage)", systemImage: "battery.100percent")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                }
                Text(snapshot.note)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                SectionCard {
                    VStack(spacing: 14) {
                        Image(systemName: "bicycle")
                            .font(.system(size: 42, weight: .light))
                            .foregroundStyle(Theme.accent.opacity(0.65))
                            .accessibilityHidden(true)
                        Text("No bicycle snapshot yet")
                            .font(.system(size: 15, weight: .semibold))
                        Text("Read your bicycle to see the component identities and paddle assignments it returns.")
                            .font(.system(size: 12))
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                            .frame(maxWidth: 370)
                        if !model.canRead {
                            Text(model.selectedDevice == nil ? "Start with the USB connection in the sidebar." : "Prepare the adapter support file to continue.")
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 24)
                }
            }
        }
    }

    private var footer: some View {
        Text("Reports contain the original JSON returned by the connection helper. SM-BCR2 does not provide the dealer battery-drain diagnostic test.")
            .font(.system(size: 10))
            .foregroundStyle(.tertiary)
            .fixedSize(horizontal: false, vertical: true)
    }
}

private struct ComponentCard: View {
    let unit: BikeUnit

    var body: some View {
        SectionCard {
            HStack(alignment: .top, spacing: 12) {
                Image(systemName: unit.symbol)
                    .font(.system(size: 20))
                    .foregroundStyle(Theme.accent)
                    .frame(width: 28, height: 30)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 4) {
                    Text(unit.model)
                        .font(.system(size: 15, weight: .semibold, design: .monospaced))
                        .textSelection(.enabled)
                    Text(unit.role)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
                Spacer()
                VStack(alignment: .trailing, spacing: 4) {
                    Text("Slot \(unit.slot)").font(.caption).foregroundStyle(.secondary)
                    if let version = unit.firmwareVersion {
                        Text("Firmware \(version)")
                            .font(.system(size: 10))
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
            }

            if let paddles = unit.paddles {
                Divider().padding(.vertical, 3)
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(paddles.returnedKeys, id: \.self) { key in
                        HStack {
                            Text(paddles.caption(for: key))
                                .font(.system(size: 12, weight: .medium))
                            Spacer()
                            Text(paddles.labels[key] ?? "")
                                .font(.system(size: 12, design: .monospaced))
                                .foregroundStyle(.secondary)
                                .textSelection(.enabled)
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }

            if let errors = unit.readErrors, !errors.isEmpty {
                DisclosureGroup("\(errors.count) fields could not be read") {
                    VStack(alignment: .leading, spacing: 8) {
                        ForEach(Array(errors.enumerated()), id: \.offset) { _, error in
                            Text(error).font(.caption).textSelection(.enabled)
                        }
                    }
                    .padding(.top, 8)
                }
                .font(.caption)
                .foregroundStyle(Theme.warning)
            }

            DisclosureGroup("Raw component identity") {
                VStack(alignment: .leading, spacing: 7) {
                    DetailRow(title: "Series / number", value: String(format: "%02x / %02x", unit.series, unit.number))
                    DetailRow(title: "Part", value: unit.partKnown ? String(format: "%02x", unit.part) : "Not read")
                    if let paddles = unit.paddles {
                        DetailRow(title: "Paddle reply", value: paddles.raw)
                    }
                }
                .padding(.top, 8)
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }
}

private struct SectionCard<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 16) { content }
            .padding(22)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.card, in: RoundedRectangle(cornerRadius: 16))
            .overlay(RoundedRectangle(cornerRadius: 16).stroke(Theme.border, lineWidth: 1))
    }
}

private struct Metric: View {
    let title: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            Text(title).font(.system(size: 10)).foregroundStyle(.secondary)
            Text(value).font(.system(size: 12, weight: .medium)).textSelection(.enabled)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct StatusPill: View {
    let title: String
    let symbol: String
    let color: Color

    var body: some View {
        Label(title, systemImage: symbol)
            .font(.system(size: 10, weight: .medium))
            .foregroundStyle(color)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(color.opacity(0.08), in: Capsule())
    }
}

private struct DetailRow: View {
    let title: String
    let value: String

    var body: some View {
        HStack(alignment: .top) {
            Text(title).frame(width: 110, alignment: .leading)
            Text(value).fontDesign(.monospaced).textSelection(.enabled)
        }
        .font(.caption)
        .accessibilityElement(children: .combine)
    }
}
