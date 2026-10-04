import Foundation

struct USBDevice: Decodable, Identifiable, Sendable {
    let bus: Int
    let address: Int
    let path: [Int]
    let vendorID: String
    let productID: String
    let usbVersion: String
    let deviceVersion: String
    let speed: String
    let configurations: [USBConfiguration]

    var id: String { "\(bus):\(address)" }
    var connectionName: String { "SM-BCR2 · Bus \(bus), address \(address)" }
    var isBehindHub: Bool { path.count > 1 }
    var supportsRuntime: Bool {
        configurations.contains { $0.number == 2 && $0.layout == "bidirectional-bulk" }
    }
    var needsSupportFile: Bool {
        configurations.count == 1 && configurations.first?.layout == "bulk-out-only"
    }

    enum CodingKeys: String, CodingKey {
        case bus, address, path, speed, configurations
        case vendorID = "vendor_id"
        case productID = "product_id"
        case usbVersion = "usb_version"
        case deviceVersion = "device_version"
    }
}

struct USBConfiguration: Decodable, Sendable {
    let number: Int
    let layout: String
}

struct AdapterInfo: Decodable, Sendable {
    let linkReplyHex: String
    let firmwareReplyHex: String
    let firmwareVersion: String
    let note: String

    enum CodingKeys: String, CodingKey {
        case linkReplyHex = "link_reply_hex"
        case firmwareReplyHex = "firmware_reply_hex"
        case firmwareVersion = "firmware_version"
        case note
    }
}

struct BikeSnapshot: Decodable, Sendable {
    let capturedAt: String
    let pcSlot: Int
    let slotBitmap: String
    let units: [BikeUnit]
    let batteryLevelRaw: Int?
    let note: String

    var date: Date? { ISO8601DateFormatter().date(from: capturedAt) }
    var readErrorCount: Int { units.reduce(0) { $0 + ($1.readErrors?.count ?? 0) } }

    enum CodingKeys: String, CodingKey {
        case capturedAt = "captured_at"
        case pcSlot = "pc_slot"
        case slotBitmap = "slot_bitmap"
        case units, note
        case batteryLevelRaw = "battery_level_raw"
    }
}

struct BikeUnit: Decodable, Identifiable, Sendable {
    let slot: Int
    let series: Int
    let number: Int
    let part: Int
    let partKnown: Bool
    let model: String
    let firmwareVersion: String?
    let paddles: PaddleSettings?
    let readErrors: [String]?

    var id: Int { slot }
    var role: String {
        switch number {
        case 0: return "Battery / master unit"
        case 1: return "Shift control"
        case 3: return "Front derailleur"
        case 4: return "Rear derailleur"
        case 8: return "Wireless unit"
        default: return "Bicycle component"
        }
    }

    var symbol: String {
        switch number {
        case 0: return "battery.100percent"
        case 1: return "hand.point.up.left"
        case 3, 4: return "gearshape"
        case 8: return "antenna.radiowaves.left.and.right"
        default: return "square.connected.to.line.below"
        }
    }

    enum CodingKeys: String, CodingKey {
        case slot, series, number, part, model, paddles
        case partKnown = "part_known"
        case firmwareVersion = "firmware_version"
        case readErrors = "read_errors"
    }
}

struct PaddleSettings: Decodable, Sendable {
    let a: Int
    let b: Int
    let c: Int?
    let s: Int?
    let raw: String
    let labels: [String: String]

    var returnedKeys: [String] { ["a", "b", "c", "s"].filter { labels[$0] != nil } }

    func caption(for key: String) -> String {
        switch key {
        case "a": return "Paddle X (A)"
        case "b": return "Paddle Y (B)"
        default: return "Switch \(key.uppercased())"
        }
    }

}

enum ReportKind: String, CaseIterable, Identifiable, Sendable {
    case usb = "USB connection"
    case adapter = "Adapter information"
    case bicycle = "Bicycle snapshot"

    var id: String { rawValue }
    var filename: String {
        switch self {
        case .usb: return "open-gears-usb.json"
        case .adapter: return "open-gears-adapter.json"
        case .bicycle: return "open-gears-bicycle.json"
        }
    }
}
