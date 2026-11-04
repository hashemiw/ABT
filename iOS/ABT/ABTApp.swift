import SwiftUI

@main
struct ABTApp: App {
    @State private var model = UploadViewModel(api: APIClient(baseURL: AppConfig.apiBaseURL, store: KeychainSessionStore()))

    var body: some Scene {
        WindowGroup { UploadView(model: model) }
    }
}

enum AppConfig {
    /// Injected per build configuration via Info.plist (see project.yml).
    static var apiBaseURL: URL {
        guard let s = Bundle.main.object(forInfoDictionaryKey: "ABTAPIBaseURL") as? String, let url = URL(string: s) else {
            preconditionFailure("ABTAPIBaseURL is missing from Info.plist")
        }
        return url
    }
}
