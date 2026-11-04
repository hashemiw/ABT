import Foundation

enum APIError: Error, Equatable, Sendable {
    case offline
    case timedOut
    case unauthorized
    case server(status: Int, code: String, message: String)
    case uploadRejected(status: Int)
    case invalidResponse

    /// Text safe to show to the person using the app; never includes URLs, tokens or backend internals.
    var userMessage: String {
        switch self {
        case .offline: "You appear to be offline. Check your connection and try again."
        case .timedOut: "The request took too long. Please try again."
        case .unauthorized: "Your session expired. Please try again."
        case let .server(status, code, message):
            // The backend's messages are written for end users; fall back for anything unexpected.
            status >= 500 && code != "SERVICE_UNAVAILABLE" ? "Something went wrong on our side. Please try again." : message
        case .uploadRejected: "The upload didn't go through. Please try again."
        case .invalidResponse: "We got an unexpected response. Please try again."
        }
    }

    /// Whether repeating the same action could plausibly succeed.
    var isRetryable: Bool {
        switch self {
        case .offline, .timedOut, .uploadRejected, .invalidResponse, .unauthorized: true
        case let .server(status, _, _): status >= 500 || status == 429 || status == 409
        }
    }

    static func from(_ error: Error) -> APIError {
        if let e = error as? APIError { return e }
        if let u = error as? URLError {
            switch u.code {
            case .timedOut: return .timedOut
            case .notConnectedToInternet, .networkConnectionLost, .cannotConnectToHost, .cannotFindHost, .dataNotAllowed: return .offline
            default: return .invalidResponse
            }
        }
        return .invalidResponse
    }
}
