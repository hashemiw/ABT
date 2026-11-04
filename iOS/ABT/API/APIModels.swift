import Foundation

// Mirrors Backend/api/openapi.yaml (v0.2.0). Optional/nullable fields are tolerated per the contract.

struct SessionResponse: Decodable, Sendable {
    struct User: Decodable, Sendable { let id: String }
    struct Session: Decodable, Sendable { let token: String; let expiresAt: Date }
    let user: User
    let session: Session
}

struct UploadIntentRequest: Encodable, Sendable {
    let filename: String
    let contentType: String
    let sizeBytes: Int
}

struct UploadIntent: Decodable, Sendable {
    struct Upload: Decodable, Sendable {
        let url: URL
        let method: String
        let headers: [String: String]
        let expiresAt: Date
    }
    let assetId: String
    let upload: Upload
}

struct CompletedMedia: Decodable, Sendable {
    let id: String
    let status: String
    let width: Int
    let height: Int
    let contentType: String
}

struct MediaAsset: Decodable, Sendable {
    let id: String
    let kind: String
    let status: String
    let width: Int?
    let height: Int?
    let contentType: String?
    let downloadUrl: URL?
    let downloadUrlExpiresAt: Date?
}

struct DeletedMedia: Decodable, Sendable {
    let id: String
    let status: String
}

struct ErrorEnvelope: Decodable, Sendable {
    struct Body: Decodable, Sendable { let code: String; let message: String; let requestId: String? }
    let error: Body
}

enum APIDecoding {
    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            let plain = ISO8601DateFormatter()
            let fractional = ISO8601DateFormatter()
            fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = plain.date(from: s) ?? fractional.date(from: s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Invalid RFC 3339 date"))
        }
        return d
    }()
    static let encoder: JSONEncoder = {
        let e = JSONEncoder()
        e.keyEncodingStrategy = .convertToSnakeCase
        return e
    }()
}
