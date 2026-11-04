import Foundation
import Observation
import UIKit

@MainActor
@Observable
final class UploadViewModel {
    enum Phase: Equatable {
        case idle
        case preparing
        case uploading
        case validating
        case ready(assetId: String)
        case deleting
        case failed(message: String, retryable: Bool)
    }

    private(set) var phase: Phase = .idle
    private(set) var preview: UIImage?
    /// Short-lived signed URL of the server-stored copy; shown to confirm the round trip.
    private(set) var storedCopyURL: URL?
    /// Non-fatal message (e.g. a failed delete) shown alongside the ready state.
    private(set) var notice: String?

    private let api: APIClient
    private var pending: (jpeg: Data, assetId: String?, intent: UploadIntent?)?
    private var task: Task<Void, Never>?

    init(api: APIClient) { self.api = api }

    var isBusy: Bool {
        switch phase {
        case .preparing, .uploading, .validating, .deleting: true
        default: false
        }
    }

    /// Takes raw picker bytes. Replaces any previous selection.
    func select(_ data: Data?) {
        task?.cancel()
        storedCopyURL = nil
        guard let data else { return fail(.invalidResponse) }
        phase = .preparing
        task = Task {
            do {
                let prepared = try await Task.detached { try ImagePreparer.prepare(data) }.value
                preview = UIImage(data: prepared.jpeg)
                pending = (prepared.jpeg, nil, nil)
                await run()
            } catch ImagePreparationError.tooLarge {
                phase = .failed(message: "This photo is too large to upload. Try a different one.", retryable: false)
            } catch {
                phase = .failed(message: "We couldn't read that file. Please choose a JPEG, PNG or HEIC photo.", retryable: false)
            }
        }
    }

    func retry() {
        guard case .failed(_, true) = phase, pending != nil else { return }
        task = Task { await run() }
    }

    func reset() {
        task?.cancel()
        pending = nil
        preview = nil
        storedCopyURL = nil
        notice = nil
        phase = .idle
    }

    func deleteUploaded() {
        guard case let .ready(id) = phase else { return }
        notice = nil
        phase = .deleting
        task = Task {
            do {
                _ = try await api.deleteMedia(id: id)
                reset()
            } catch {
                // The asset is still uploaded; stay on the ready screen so deletion can be retried.
                notice = APIError.from(error).userMessage
                phase = .ready(assetId: id)
            }
        }
    }

    /// Resumable: repeating after a failure reuses the intent/asset where that is still valid.
    private func run() async {
        guard var p = pending else { return }
        do {
            if p.intent == nil || (p.intent?.upload.expiresAt ?? .distantPast).timeIntervalSinceNow < 30 {
                phase = .uploading
                let intent = try await api.createUploadIntent(filename: "photo.jpg", contentType: "image/jpeg", sizeBytes: p.jpeg.count)
                p.intent = intent
                p.assetId = intent.assetId
                pending = p
            }
            guard let intent = p.intent else { return }
            phase = .uploading
            try await api.upload(p.jpeg, to: intent.upload)
            phase = .validating
            let done = try await api.completeMedia(id: intent.assetId)
            storedCopyURL = try? await api.media(id: done.id).downloadUrl
            phase = .ready(assetId: done.id)
        } catch is CancellationError {
            return
        } catch {
            if (error as? URLError)?.code == .cancelled { return }
            fail(APIError.from(error))
        }
    }

    private func fail(_ error: APIError) {
        // An expired or rejected intent must be re-requested rather than reused.
        if case .uploadRejected = error { pending?.intent = nil }
        phase = .failed(message: error.userMessage, retryable: error.isRetryable)
    }
}
