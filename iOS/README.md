# ABT iOS (M1)

SwiftUI, iOS 17+, no third-party dependencies. The Xcode project is generated from `project.yml` by [XcodeGen](https://github.com/yonaskolb/XcodeGen) (`brew install xcodegen`); `ABT.xcodeproj` is committed for convenience, regenerate with `xcodegen generate` after adding files.

```sh
cd iOS
xcodegen generate
xcodebuild -project ABT.xcodeproj -scheme ABT -destination 'platform=iOS Simulator,name=iPhone 18 Pro' CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual test
```

Ad-hoc signing is required: unsigned simulator builds cannot use the Keychain (error -34018).

## Running against the local backend

Start the backend (`Backend/README.md`), then run the app in a simulator. Debug builds use `http://127.0.0.1:8080` (build setting `ABT_API_BASE_URL`); signed storage URLs come from the backend's `STORAGE_PUBLIC_ENDPOINT` (`http://127.0.0.1:9000` by default), which the simulator reaches directly. On a real device, set `ABT_API_BASE_URL` and the backend's `STORAGE_PUBLIC_ENDPOINT` to this Mac's LAN address.

ATS: only `NSAllowsLocalNetworking` is enabled (loopback/LAN hosts). `NSAllowsArbitraryLoads` is not used; Release points at an HTTPS placeholder until the production origin is chosen.

## Structure

- `API/` — `APIClient` (actor; owns the anonymous session, retries once on 401), contract models, `APIError` with user-safe messages.
- `Storage/` — `SessionStore` protocol and Keychain implementation (device-only, after-first-unlock).
- `Upload/` — `ImagePreparer` (ImageIO: orientation baked in, downscale to 2048px, JPEG), `UploadViewModel` state machine, `UploadView`.

Anonymous sessions cannot be refreshed: after expiry/revocation a new user is created and earlier uploads are no longer reachable (reinstall recovery is not guaranteed in the MVP, per the specs).
