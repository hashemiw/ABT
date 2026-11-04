import PhotosUI
import SwiftUI

struct UploadView: View {
    @Bindable var model: UploadViewModel
    @State private var item: PhotosPickerItem?

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 20) {
                    photoArea
                    statusArea
                    actions
                }
                .padding()
            }
            .navigationTitle("Your Photo")
            .onChange(of: item) { _, newItem in
                guard let newItem else { return }
                Task {
                    let data = try? await newItem.loadTransferable(type: Data.self)
                    model.select(data)
                    item = nil
                }
            }
        }
    }

    @ViewBuilder private var photoArea: some View {
        if let preview = model.preview {
            Image(uiImage: preview)
                .resizable().scaledToFit()
                .frame(maxHeight: 360)
                .clipShape(RoundedRectangle(cornerRadius: 12))
                .accessibilityLabel("Selected photo preview")
        } else {
            RoundedRectangle(cornerRadius: 12)
                .fill(.quaternary)
                .frame(height: 240)
                .overlay {
                    VStack(spacing: 8) {
                        Image(systemName: "person.crop.rectangle").font(.largeTitle)
                        Text("Choose a photo of yourself to get started").multilineTextAlignment(.center)
                    }.foregroundStyle(.secondary).padding()
                }
                .accessibilityElement(children: .combine)
        }
    }

    @ViewBuilder private var statusArea: some View {
        switch model.phase {
        case .idle: EmptyView()
        case .preparing: busy("Preparing your photo…")
        case .uploading: busy("Uploading securely…")
        case .validating: busy("Checking your photo…")
        case .deleting: busy("Deleting…")
        case .ready:
            VStack(spacing: 12) {
                Label("Photo uploaded and checked", systemImage: "checkmark.seal.fill").foregroundStyle(.green)
                if let notice = model.notice {
                    Text(notice).font(.footnote).foregroundStyle(.red)
                }
                if let url = model.storedCopyURL {
                    AsyncImage(url: url) { phase in
                        switch phase {
                        case let .success(image): image.resizable().scaledToFit().frame(maxHeight: 120).clipShape(RoundedRectangle(cornerRadius: 8))
                        case .failure: Text("Stored copy unavailable").font(.footnote).foregroundStyle(.secondary)
                        default: ProgressView()
                        }
                    }
                    .accessibilityLabel("Stored copy of your photo")
                    Text("Stored copy").font(.caption).foregroundStyle(.secondary)
                }
            }
        case let .failed(message, _):
            Label(message, systemImage: "exclamationmark.triangle.fill").foregroundStyle(.red).multilineTextAlignment(.center)
                .accessibilityAddTraits(.isStaticText)
        }
    }

    private func busy(_ text: String) -> some View {
        HStack(spacing: 12) { ProgressView(); Text(text) }.accessibilityElement(children: .combine)
    }

    private var picker: some View {
        PhotosPicker(selection: $item, matching: .images, photoLibrary: .shared()) {
            Label(model.preview == nil ? "Choose Photo" : "Choose a Different Photo", systemImage: "photo.on.rectangle")
        }
        .disabled(model.isBusy)
    }

    @ViewBuilder private var actions: some View {
        VStack(spacing: 12) {
            if case .failed(_, true) = model.phase {
                Button("Try again") { model.retry() }.buttonStyle(.borderedProminent)
            }
            if model.preview == nil {
                picker.buttonStyle(.borderedProminent)
            } else {
                picker.buttonStyle(.bordered)
            }
            if case .ready = model.phase {
                Button("Delete Uploaded Photo", role: .destructive) { model.deleteUploaded() }
            }
        }
    }
}
