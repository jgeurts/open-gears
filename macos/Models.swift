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
    let adapter: AdapterIdentity?

    var date: Date? { ISO8601DateFormatter().date(from: capturedAt) }
    var readErrorCount: Int { units.reduce(0) { $0 + ($1.readErrors?.count ?? 0) } }

    enum CodingKeys: String, CodingKey {
        case capturedAt = "captured_at"
        case pcSlot = "pc_slot"
        case slotBitmap = "slot_bitmap"
        case units, note, adapter
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
    let paddleEditSupported: Bool?
    let paddleEditReason: String?

    var canEditPaddles: Bool { paddleEditSupported == true && paddles != nil }
    var shifterName: String {
        guard number == 1, partKnown, model.hasPrefix("ST-") else { return model }
        if model.hasSuffix("-L") { return "Left shifter" }
        if model.hasSuffix("-R") { return "Right shifter" }
        return model
    }
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
        case paddleEditSupported = "paddle_edit_supported"
        case paddleEditReason = "paddle_edit_reason"
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
    case paddles = "Paddle apply result"

    var id: String { rawValue }
    var filename: String {
        switch self {
        case .usb: return "open-gears-usb.json"
        case .adapter: return "open-gears-adapter.json"
        case .bicycle: return "open-gears-bicycle.json"
        case .paddles: return "open-gears-paddle-result.json"
        }
    }
}


struct AdapterIdentity: Codable, Equatable, Sendable {
    let bus: Int
    let address: Int?
    let path: [Int]
}

enum PaddleKey: String, Sendable {
    case x, y
    var caption: String { self == .x ? "X" : "Y" }
}

enum PaddleFunction {
    static let standard = [0, 1, 2, 3]

    static func label(_ value: Int) -> String {
        switch value {
        case 0: return "Front shift up"
        case 1: return "Front shift down"
        case 2: return "Rear shift up"
        case 3: return "Rear shift down"
        case 15: return "Unassigned"
        default: return "Model-specific assignment (\(value))"
        }
    }
}

struct PaddleDraft: Equatable, Sendable {
    var a: Int
    var b: Int
}

struct PendingPaddleChange: Identifiable, Sendable {
    let slot: Int
    let model: String
    let key: PaddleKey
    let before: Int
    let after: Int
    var id: String { "\(slot):\(key.rawValue)" }
}

struct PaddlePlan: Encodable, Sendable {
    let version = 1
    let adapter: PhysicalAdapter
    let changes: [Change]

    struct PhysicalAdapter: Encodable, Sendable {
        let bus: Int
        let path: [Int]
    }

    struct Change: Encodable, Sendable {
        let slot: Int
        let series: Int
        let number: Int
        let part: Int
        let firmwareVersion: String
        let beforeRaw: String
        let a: Int
        let b: Int

        enum CodingKeys: String, CodingKey {
            case slot, series, number, part, a, b
            case firmwareVersion = "firmware_version"
            case beforeRaw = "before_raw"
        }
    }
}

struct PaddleApplyResult: Decodable, Sendable {
    let status: String
    let changes: [Change]
    let snapshot: BikeSnapshot?
    let error: String?

    struct Change: Decodable, Sendable {
        let slot: Int
        let status: String
        let beforeRaw: String
        let requestedRaw: String
        let actualRaw: String?
        let error: String?
        let warning: String?

        enum CodingKeys: String, CodingKey {
            case slot, status, error, warning
            case beforeRaw = "before_raw"
            case requestedRaw = "requested_raw"
            case actualRaw = "actual_raw"
        }
    }
}
