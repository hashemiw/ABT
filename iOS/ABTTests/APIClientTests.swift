import Foundation
import Testing
@testable import ABT

/// Serves canned responses so no network or backend is needed.
final class StubProtocol: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) -> (Int, Data))?
    nonisolated(unsafe) static var seen: [URLRequest] = []
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        var req = request
        if let stream = request.httpBodyStream { // upload bodies arrive as streams
            stream.open(); var d = Data(); var buf = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable { let n = stream.read(&buf, maxLength: buf.count); if n <= 0 { break }; d.append(buf, count: n) }
            req.httpBody = d
        }
        Self.seen.append(req)
        let (status, body) = Self.handler?(req) ?? (500, Data())
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

final class MemoryStore: SessionStore, @unchecked Sendable {
    private let lock = NSLock()
    private var value: StoredSession?
    init(_ s: StoredSession? = nil) { value = s }
    func load() -> StoredSession? { lock.withLock { value } }
    func save(_ s: StoredSession) throws { lock.withLock { value = s } }
    func clear() { lock.withLock { value = nil } }
}

func stubbedClient(store: SessionStore) -> APIClient {
    let c = URLSessionConfiguration.ephemeral
    c.protocolClasses = [StubProtocol.self]
    return APIClient(baseURL: URL(string: "http://test.local")!, store: store, session: URLSession(configuration: c))
}

private func json(_ s: String) -> Data { Data(s.utf8) }
private let sessionJSON = #"{"user":{"id":"u1"},"session":{"token":"tok-NEW","expires_at":"2099-01-01T00:00:00Z"}}"#

@Suite(.serialized) struct APIClientTests {
    @Test func createsAndStoresSessionThenSendsBearer() async throws {
        StubProtocol.seen = []
        StubProtocol.handler = { req in
            if req.url!.path == "/v1/sessions" { return (201, json(sessionJSON)) }
            return (200, json(#"{"id":"a1","kind":"original","status":"ready","width":10,"height":20,"content_type":"image/jpeg","download_url":"http://x/y","download_url_expires_at":"2099-01-01T00:00:00Z"}"#))
        }
        let store = MemoryStore()
        let api = stubbedClient(store: store)
        let asset = try await api.media(id: "a1")
        #expect(asset.width == 10 && asset.downloadUrl?.absoluteString == "http://x/y")
        #expect(store.load()?.token == "tok-NEW")
        #expect(StubProtocol.seen.last?.value(forHTTPHeaderField: "Authorization") == "Bearer tok-NEW")
    }

    @Test func unauthorizedStartsNewSessionAndRetriesOnce() async throws {
        StubProtocol.seen = []
        let old = StoredSession(userId: "u0", token: "tok-OLD", expiresAt: .distantFuture)
        StubProtocol.handler = { req in
            if req.url!.path == "/v1/sessions" { return (201, json(sessionJSON)) }
            if req.value(forHTTPHeaderField: "Authorization") == "Bearer tok-OLD" { return (401, json(#"{"error":{"code":"UNAUTHORIZED","message":"x","request_id":"r"}}"#)) }
            return (200, json(#"{"id":"a1","status":"deletion_pending"}"#))
        }
        let store = MemoryStore(old)
        let r = try await stubbedClient(store: store).deleteMedia(id: "a1")
        #expect(r.status == "deletion_pending")
        #expect(store.load()?.token == "tok-NEW")
        #expect(StubProtocol.seen.count == 3)
    }

    @Test func decodesServerErrorEnvelope() async throws {
        StubProtocol.handler = { _ in (422, json(#"{"error":{"code":"MEDIA_INVALID","message":"Too small.","request_id":"r"}}"#)) }
        let api = stubbedClient(store: MemoryStore(StoredSession(userId: "u", token: "t", expiresAt: .distantFuture)))
        await #expect(throws: APIError.server(status: 422, code: "MEDIA_INVALID", message: "Too small.")) { try await api.completeMedia(id: "a1") }
        #expect(APIError.server(status: 500, code: "INTERNAL_ERROR", message: "boom").userMessage.contains("our side"))
    }

    @Test func uploadHasSignedHeadersAndNoBearer() async throws {
        StubProtocol.seen = []
        StubProtocol.handler = { _ in (200, Data()) }
        let api = stubbedClient(store: MemoryStore(StoredSession(userId: "u", token: "secret", expiresAt: .distantFuture)))
        let up = UploadIntent.Upload(url: URL(string: "http://storage.local/b/k?X-Amz-Signature=s")!, method: "PUT", headers: ["Content-Type": "image/jpeg"], expiresAt: .distantFuture)
        try await api.upload(Data([1, 2, 3]), to: up)
        let req = try #require(StubProtocol.seen.last)
        #expect(req.httpMethod == "PUT")
        #expect(req.value(forHTTPHeaderField: "Content-Type") == "image/jpeg")
        #expect(req.value(forHTTPHeaderField: "Authorization") == nil)
        StubProtocol.handler = { _ in (403, Data()) }
        await #expect(throws: APIError.uploadRejected(status: 403)) { try await api.upload(Data([1]), to: up) }
    }

    @Test func decodesContractDates() throws {
        let r = try APIDecoding.decoder.decode(SessionResponse.self, from: json(sessionJSON))
        #expect(r.session.expiresAt.timeIntervalSince1970 == 4_070_908_800)
    }
}
