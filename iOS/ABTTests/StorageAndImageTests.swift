import Foundation
import ImageIO
import Testing
import UIKit
@testable import ABT

struct KeychainTests {
    @Test func roundTripsAndClears() {
        let store = KeychainSessionStore(service: "app.abt.tests.\(UUID().uuidString)")
        defer { store.clear() }
        #expect(store.load() == nil)
        let s = StoredSession(userId: "u", token: "t", expiresAt: Date(timeIntervalSince1970: 4_000_000_000))
        #expect(throws: Never.self) { try store.save(s) }
        #expect(store.load() == s)
        let s2 = StoredSession(userId: "u", token: "t2", expiresAt: s.expiresAt)
        try? store.save(s2)
        #expect(store.load() == s2)
        store.clear()
        #expect(store.load() == nil)
    }
}

struct ImagePreparerTests {
    private func jpeg(width: Int, height: Int, orientation: CGImagePropertyOrientation) -> Data {
        let img = UIGraphicsImageRenderer(size: CGSize(width: width, height: height), format: { let f = UIGraphicsImageRendererFormat(); f.scale = 1; return f }()).image { ctx in
            UIColor.red.setFill(); ctx.fill(CGRect(x: 0, y: 0, width: width, height: height))
        }
        let data = NSMutableData()
        let dest = CGImageDestinationCreateWithData(data, "public.jpeg" as CFString, 1, nil)!
        CGImageDestinationAddImage(dest, img.cgImage!, [kCGImagePropertyOrientation: orientation.rawValue] as CFDictionary)
        CGImageDestinationFinalize(dest)
        return data as Data
    }

    @Test func bakesOrientationAndDownscales() throws {
        // 400x200 stored with orientation "right" displays as 200x400.
        let (out, size) = try ImagePreparer.prepare(jpeg(width: 400, height: 200, orientation: .right), maxLongSide: 100)
        #expect(size.width == 50 && size.height == 100)
        let decoded = try #require(UIImage(data: out))
        #expect(decoded.imageOrientation == .up)
    }

    @Test func rejectsNonImages() {
        #expect(throws: ImagePreparationError.unreadable) { try ImagePreparer.prepare(Data("not an image".utf8)) }
    }
}
