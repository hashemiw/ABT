import Foundation
import ImageIO
import UIKit

enum ImagePreparationError: Error, Equatable {
    case unreadable
    case tooLarge
}

/// Converts any picked image (JPEG, PNG, HEIC...) into upright, metadata-free JPEG bytes the backend accepts.
/// The picker's original data is never modified or written anywhere.
enum ImagePreparer {
    static let maxLongSide = 2048
    static let maxBytes = 10 * 1024 * 1024  // matches the backend's default MAX_UPLOAD_BYTES

    static func prepare(_ data: Data, maxLongSide: Int = ImagePreparer.maxLongSide) throws -> (jpeg: Data, size: CGSize) {
        guard let src = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary) else { throw ImagePreparationError.unreadable }
        // Decodes at reduced size and bakes EXIF orientation into the pixels (the server drops EXIF).
        let opts: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxLongSide,
        ]
        guard let cg = CGImageSourceCreateThumbnailAtIndex(src, 0, opts as CFDictionary) else { throw ImagePreparationError.unreadable }
        let image = UIImage(cgImage: cg, scale: 1, orientation: .up)
        for quality in [0.9, 0.7, 0.5] {
            if let jpeg = image.jpegData(compressionQuality: quality), jpeg.count <= maxBytes {
                return (jpeg, CGSize(width: cg.width, height: cg.height))
            }
        }
        throw ImagePreparationError.tooLarge
    }
}
