import SwiftUI

@main
struct OpenGearsApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup("Open Gears") {
            ContentView(model: model)
                .frame(minWidth: 720, minHeight: 620)
        }
        .defaultSize(width: 860, height: 760)
        .commands {
            CommandGroup(replacing: .newItem) { }
            CommandMenu("Bicycle") {
                Button("Connect Bicycle") { model.connectBicycle() }
                    .keyboardShortcut("r", modifiers: .command)
                    .disabled(model.isBusy)
                Button("Apply Paddle Changes") { model.applyPaddleChanges() }
                    .keyboardShortcut("s", modifiers: .command)
                    .disabled(!model.canApply)
                Divider()
                Button("Check USB Connection") { model.discover() }
                    .disabled(model.isBusy)
                Button("Choose Adapter Support File…") { model.chooseSupportFile() }
                    .disabled(model.isBusy)
                Button("Read Adapter Information") { model.readAdapter() }
                    .disabled(!model.canRead)
            }
        }
    }
}

struct ContentView: View {
    @ObservedObject var model: AppModel
    @State private var showsAdvanced = false

    var body: some View {
        VStack(spacing: 0) {
            connectionBar
            Divider()
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    if model.isBusy {
                        HStack(spacing: 12) {
                            ProgressView().controlSize(.small)
                            Text(model.activity)
                        }
                        .accessibilityElement(children: .combine)
                        .accessibilityLabel("In progress: \(model.activity)")
                    }
                    if let error = model.friendlyErrorMessage { errorMessage(error) }
                    if let notice = model.notice {
                        Label(notice, systemImage: model.applyVerified ? "checkmark.circle" : "info.circle")
                            .font(.callout)
                            .textSelection(.enabled)
                            .accessibilityLabel(notice)
                    }
                    if let bicycle = model.bicycle {
                        bicycleControls(bicycle)
                    } else if model.errorMessage == nil {
                        connectionInstructions
                    }
                    capabilities
                    advanced
                }
                .padding(24)
                .frame(maxWidth: 920, alignment: .leading)
                .frame(maxWidth: .infinity, alignment: .topLeading)
            }
            .background(Color(nsColor: .windowBackgroundColor))
            if model.hasPendingChanges { applyBar }
        }
        .task {
            if !model.hasCheckedConnection && !model.isBusy { model.discover() }
        }
    }

    private var connectionBar: some View {
        HStack(spacing: 16) {
            Image(systemName: "bicycle")
                .font(.title)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(model.bicycle != nil && model.bicycleIsCurrent ? "Bicycle connected" : "No bicycle connected")
                    .font(.title3.weight(.semibold))
                Text(model.connectionSummary)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            settingsMenu
            Button(model.bicycle != nil ? "Refresh bicycle" : "Connect bicycle", action: model.connectBicycle)
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                .disabled(model.isBusy)
                .keyboardShortcut("r", modifiers: .command)
                .accessibilityHint("Find the SM-BCR2 adapter and read the bicycle's components and paddle assignments")
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 20)
    }

    private var connectionInstructions: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Connect to your bicycle")
                .font(.headline)
            Text("Plug the SM-BCR2 into your bicycle’s charging port and your Mac using a USB data cable. The bicycle needs a charged battery.")
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
    }

    private var settingsMenu: some View {
        Menu {
            Button(model.supportFile == nil ? "Choose adapter support file…" : "Change adapter support file…", action: model.chooseSupportFile)
                .disabled(model.isBusy)
            if model.supportFile != nil {
                Button("Forget adapter support file", action: model.forgetSupportFile)
                    .disabled(model.isBusy)
            }
            Divider()
            if model.devices.count > 1 {
                Menu("Choose USB adapter") {
                    ForEach(model.devices) { device in
                        Button(action: { model.selectDevice(device.id) }) {
                            if device.id == model.selectedID {
                                Label(device.connectionName, systemImage: "checkmark")
                            } else {
                                Text(device.connectionName)
                            }
                        }
                        .disabled(model.isBusy)
                    }
                }
            }
            Button("Check USB connection", action: model.discover).disabled(model.isBusy)
            Button("Read adapter information", action: model.readAdapter).disabled(!model.canRead)
        } label: {
            Label("Settings", systemImage: "gearshape").labelStyle(.iconOnly)
        }
        .menuStyle(.borderlessButton)
        .fixedSize()
        .help("Settings")
        .accessibilityLabel("Settings")
    }

    private func bicycleControls(_ bicycle: BikeSnapshot) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(alignment: .firstTextBaseline) {
                Text("Paddle assignments").font(.title2.weight(.semibold))
                Spacer()
                if let date = bicycle.date {
                    Text("Read \(date.formatted(date: .omitted, time: .shortened))")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            }
            if !model.bicycleIsCurrent {
                Label("This information needs a fresh read before changes can be applied.", systemImage: "arrow.clockwise")
                    .font(.callout)
            }
            let shifters = bicycle.units.filter { $0.number == 1 }
            if shifters.isEmpty {
                Text("No shifters were identified. Check the bicycle connection and refresh.")
                    .foregroundStyle(.secondary)
            }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 360), spacing: 16, alignment: .top)], alignment: .leading, spacing: 16) {
                ForEach(shifters) { unit in
                    ShifterEditor(unit: unit, model: model)
                }
            }
            if !bicycle.units.filter({ $0.number != 1 }).isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Bicycle components").font(.headline)
                    ForEach(bicycle.units.filter { $0.number != 1 }) { unit in
                        HStack(spacing: 12) {
                            Image(systemName: unit.symbol).frame(width: 24).accessibilityHidden(true)
                            Text(unit.model).fontWeight(.medium)
                            Text(unit.role).foregroundStyle(.secondary)
                            Spacer()
                            if let version = unit.firmwareVersion {
                                Text("Firmware \(version)").foregroundStyle(.secondary)
                            }
                        }
                        .font(.callout)
                        .accessibilityElement(children: .combine)
                    }
                }
                .padding(.top, 8)
            }
        }
    }

    private var applyBar: some View {
        VStack(alignment: .leading, spacing: 12) {
            Divider()
            VStack(alignment: .leading, spacing: 6) {
                ForEach(model.pendingChanges) { change in
                    let name = model.bicycle?.units.first(where: { $0.slot == change.slot })?.shifterName ?? change.model
                    Text("\(name) · Paddle \(change.key.caption): \(PaddleFunction.label(change.before)) → \(PaddleFunction.label(change.after))")
                        .font(.callout)
                }
            }
            .padding(.horizontal, 24)
            HStack(alignment: .center, spacing: 16) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("\(model.pendingChanges.count) paddle \(model.pendingChanges.count == 1 ? "change" : "changes")")
                        .font(.headline)
                    if model.isBusy {
                        HStack(spacing: 8) {
                            ProgressView().controlSize(.mini)
                            Text(model.activity)
                        }
                        .font(.callout)
                    } else {
                        Text("Apply writes these assignments, then reads them back to verify.")
                            .font(.callout)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                Button("Discard changes", action: model.discardPaddleChanges)
                    .disabled(model.isBusy)
                Button("Apply changes", action: model.applyPaddleChanges)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
                    .disabled(!model.canApply)
                    .keyboardShortcut("s", modifiers: .command)
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 16)
        }
        .background(.bar)
    }

    private var capabilities: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("What you can do").font(.headline)
            Label("Read component models, firmware versions and supported paddle assignments", systemImage: "checkmark")
            if let bicycle = model.bicycle {
                if bicycle.units.contains(where: \.canEditPaddles) {
                    Label("Change X and Y shift assignments on the supported shifters above", systemImage: "checkmark")
                } else {
                    Label("Paddle editing is unavailable for the components identified above", systemImage: "minus.circle")
                }
            } else {
                Label("Change X and Y shift assignments after a supported shifter is identified", systemImage: "hand.point.up.left")
            }
            Text("Extra buttons, shift modes and firmware updates are not available yet.")
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .font(.callout)
    }

    private var advanced: some View {
        DisclosureGroup("Advanced connection details", isExpanded: $showsAdvanced) {
            VStack(alignment: .leading, spacing: 16) {
                Text(model.selectedDevice?.connectionName ?? "No USB adapter selected")
                if let info = model.adapterInfo {
                    LabeledContent("Adapter firmware", value: info.firmwareVersion)
                }
                Menu("Export diagnostic report") {
                    ForEach(model.availableReports) { kind in
                        Button(kind.rawValue) { model.export(kind) }
                    }
                }
                .disabled(model.availableReports.isEmpty || model.isBusy)
                if let error = model.errorMessage, error != model.friendlyErrorMessage {
                    DisclosureGroup("Last connection error") {
                        Text(error).font(.caption.monospaced()).textSelection(.enabled).padding(.top, 8)
                    }
                }
                if let bicycle = model.bicycle {
                    ForEach(bicycle.units) { unit in
                        DisclosureGroup("\(unit.model) · slot \(unit.slot)") {
                            VStack(alignment: .leading, spacing: 6) {
                                Text(String(format: "Identity %02x/%02x/%02x", unit.series, unit.number, unit.part))
                                if let paddles = unit.paddles { Text("Paddle reply: \(paddles.raw)") }
                                ForEach(unit.readErrors ?? [], id: \.self) { Text($0) }
                            }
                            .font(.caption.monospaced())
                            .textSelection(.enabled)
                            .padding(.top, 8)
                        }
                    }
                }
            }
            .font(.callout)
            .padding(.top, 16)
        }
        .foregroundStyle(.secondary)
    }

    private func errorMessage(_ message: String) -> some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: "exclamationmark.triangle")
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 6) {
                Text(model.applyUncertain ? "Check the bicycle before making more changes" : model.lastOperationWasApply ? "Paddle changes need attention" : "Connection needs attention")
                    .font(.headline)
                Text(message).textSelection(.enabled)
                if model.applyUncertain {
                    Text(model.applyRecoveryExplanation)
                    if let result = model.lastApplyResult {
                        ForEach(result.changes, id: \.slot) { change in
                            let name = model.bicycle?.units.first(where: { $0.slot == change.slot })?.shifterName ?? "Shifter \(change.slot)"
                            Label("\(name): \(change.status == "verified" ? "requested assignments verified" : "refresh to check assignments")",
                                  systemImage: change.status == "verified" ? "checkmark.circle" : "arrow.clockwise")
                        }
                    }
                } else if model.lastOperationWasApply {
                    Text("Refresh the bicycle to read its current assignments, then review and select your changes again.")
                } else if model.errorMessage?.contains("no bicycle components detected") != true {
                    Text("Check both cables and the bicycle battery, then reconnect. If the adapter is unavailable, unplug and reconnect its USB cable.")
                }
            }
            Spacer()
            Button("Dismiss", action: model.dismissError)
                .buttonStyle(.borderless)
        }
        .font(.callout)
        .padding(16)
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
    }
}

private struct ShifterEditor: View {
    let unit: BikeUnit
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            HStack(alignment: .firstTextBaseline) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(unit.shifterName).font(.headline)
                    if unit.shifterName != unit.model {
                        Text(unit.model).font(.callout).foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if let firmware = unit.firmwareVersion {
                    Text("Firmware \(firmware)").font(.callout).foregroundStyle(.secondary)
                }
            }
            if let paddles = unit.paddles {
                VStack(alignment: .leading, spacing: 12) {
                    assignment("X", current: paddles.a, key: .x)
                    assignment("Y", current: paddles.b, key: .y)
                }
                if !unit.canEditPaddles {
                    Text(unit.paddleEditReason ?? "Editing is not supported for this shifter yet.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            } else {
                Text("Paddle assignments could not be read for this shifter.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(20)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(nsColor: .controlBackgroundColor), in: RoundedRectangle(cornerRadius: 12))
    }

    private func assignment(_ name: String, current: Int, key: PaddleKey) -> some View {
        return HStack(alignment: .center, spacing: 16) {
            Text("Paddle \(name)").fontWeight(.medium)
            Spacer()
            if unit.canEditPaddles {
                Picker("\(unit.model), paddle \(name)", selection: Binding(
                    get: { model.paddleValue(slot: unit.slot, key: key) ?? current },
                    set: { model.setPaddle(slot: unit.slot, key: key, value: $0) }
                )) {
                    if !PaddleFunction.standard.contains(current) {
                        Text("\(PaddleFunction.label(current)) (current)").tag(current)
                    }
                    ForEach(PaddleFunction.standard, id: \.self) { value in
                        Text("\(PaddleFunction.label(value))\(value == current ? " (current)" : "")").tag(value)
                    }
                }
                .labelsHidden()
                .frame(width: 215)
                .disabled(model.isBusy || !model.bicycleIsCurrent)
                .accessibilityLabel("\(unit.model), paddle \(name) assignment")
            } else {
                Text(PaddleFunction.label(current))
            }
        }
    }
}
