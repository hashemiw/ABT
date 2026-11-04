import Foundation

/// Talks to the ABT backend. Owns the anonymous session: it is created lazily, kept in the
/// Keychain, and re-created once if the server reports it invalid.
actor APIClient {
    private let baseURL: URL
    private let http: URLSession
    private let store: SessionStore
    private var sessionTask: Task<StoredSession, Error>?

    init(baseURL: URL, store: SessionStore, session: URLSession = APIClient.makeSession()) {
        self.baseURL = baseURL
        self.store = store
        self.http = session
    }

    /// Ephemeral: no disk cache or cookies, so signed URLs and responses are never persisted.
    static func makeSession() -> URLSession {
        let c = URLSessionConfiguration.ephemeral
        c.timeoutIntervalForRequest = 30
        c.timeoutIntervalForResource = 120
        c.waitsForConnectivity = false
        return URLSession(configuration: c)
    }

    // MARK: Session

    func ensureSession() async throws -> StoredSession {
        if let s = store.load(), s.expiresAt.timeIntervalSinceNow > 60 { return s }
        if let task = sessionTask { return try await task.value }
        let task = Task { try await self.createSession() }
        sessionTask = task
        defer { sessionTask = nil }
        return try await task.value
    }

    private func createSession() async throws -> StoredSession {
        let r: SessionResponse = try await send("POST", "/v1/sessions", body: [String: String](), token: nil)
        let s = StoredSession(userId: r.user.id, token: r.session.token, expiresAt: r.session.expiresAt)
        try? store.save(s)
        return s
    }

    // MARK: Media

    func createUploadIntent(filename: String, contentType: String, sizeBytes: Int) async throws -> UploadIntent {
        try await authorized("POST", "/v1/media/upload-intents", body: UploadIntentRequest(filename: filename, contentType: contentType, sizeBytes: sizeBytes))
    }

    /// Sends bytes straight to object storage. No bearer token: the signed URL is the credential.
    func upload(_ data: Data, to upload: UploadIntent.Upload) async throws {
        var req = URLRequest(url: upload.url)
        req.httpMethod = upload.method
        for (k, v) in upload.headers { req.setValue(v, forHTTPHeaderField: k) }
        do {
            let (_, resp) = try await http.upload(for: req, from: data)
            guard let status = (resp as? HTTPURLResponse)?.statusCode else { throw APIError.invalidResponse }
            guard (200..<300).contains(status) else { throw APIError.uploadRejected(status: status) }
        } catch { throw APIError.from(error) }
    }

    func completeMedia(id: String) async throws -> CompletedMedia {
        try await authorized("POST", "/v1/media/\(id)/complete", body: [String: String]())
    }

    func media(id: String) async throws -> MediaAsset {
        try await authorized("GET", "/v1/media/\(id)", body: Optional<String>.none)
    }

    func deleteMedia(id: String) async throws -> DeletedMedia {
        try await authorized("DELETE", "/v1/media/\(id)", body: Optional<String>.none)
    }

    // MARK: Plumbing

    private func authorized<T: Decodable, B: Encodable>(_ method: String, _ path: String, body: B?) async throws -> T {
        let s = try await ensureSession()
        do {
            return try await send(method, path, body: body, token: s.token)
        } catch APIError.unauthorized {
            // Expired/revoked: anonymous sessions can't be refreshed, so start a new one and retry once.
            store.clear()
            let fresh = try await ensureSession()
            return try await send(method, path, body: body, token: fresh.token)
        }
    }

    private func send<T: Decodable, B: Encodable>(_ method: String, _ path: String, body: B?, token: String?) async throws -> T {
        var req = URLRequest(url: baseURL.appending(path: path))
        req.httpMethod = method
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        if let body {
            req.httpBody = try APIDecoding.encoder.encode(body)
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let data: Data, resp: URLResponse
        do { (data, resp) = try await http.data(for: req) } catch { throw APIError.from(error) }
        guard let status = (resp as? HTTPURLResponse)?.statusCode else { throw APIError.invalidResponse }
        guard (200..<300).contains(status) else {
            if status == 401 { throw APIError.unauthorized }
            if let env = try? APIDecoding.decoder.decode(ErrorEnvelope.self, from: data) {
                throw APIError.server(status: status, code: env.error.code, message: env.error.message)
            }
            throw APIError.server(status: status, code: "UNKNOWN", message: "Something went wrong. Please try again.")
        }
        do { return try APIDecoding.decoder.decode(T.self, from: data) } catch { throw APIError.invalidResponse }
    }
}
