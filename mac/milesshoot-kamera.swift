// Pembantu kamera kabel (PTP/tethering) untuk macOS.
//
// Kamera yang dicolok dalam mode tether (Nikon, Canon, dll) TIDAK muncul sebagai drive,
// jadi tidak bisa dipantau sebagai folder biasa. Program kecil ini memakai ImageCaptureCore
// (API resmi macOS — yang sama dipakai aplikasi Image Capture bawaan) untuk:
//   - mendeteksi kamera tersambung/terlepas
//   - mengunduh foto BARU yang dijepret ke folder yang diberikan
//
// Dipanggil: milesshoot-kamera <folder-tujuan>
// Keluaran : satu JSON per baris ke stdout, dibaca oleh proses Go induknya.
//   {"event":"ready"}
//   {"event":"connected","model":"NIKON Z 5"}
//   {"event":"file","path":"/.../DSC_0001.JPG"}
//   {"event":"disconnected"}
//   {"event":"error","msg":"..."}
//
// Foto yang SUDAH ADA di kamera sengaja diabaikan — hanya jepretan baru yang diunduh.

import Foundation
import ImageCaptureCore

func emit(_ obj: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: obj),
          let line = String(data: data, encoding: .utf8) else { return }
    print(line)
    fflush(stdout)
}

final class CameraHelper: NSObject {
    let outDir: URL
    private var browser: ICDeviceBrowser?
    private var camera: ICCameraDevice?
    // Nama berkas yang sudah ada saat kamera tersambung — diabaikan.
    private var baseline = Set<String>()
    private var baselineReady = false

    init(outDir: URL) {
        self.outDir = outDir
        super.init()
    }

    func start() {
        try? FileManager.default.createDirectory(at: outDir, withIntermediateDirectories: true)
        let b = ICDeviceBrowser()
        b.delegate = self
        b.browsedDeviceTypeMask = ICDeviceTypeMask(
            rawValue: ICDeviceTypeMask.camera.rawValue | ICDeviceLocationTypeMask.local.rawValue)!
        b.start()
        browser = b
        emit(["event": "ready"])
    }

    private func download(_ file: ICCameraFile) {
        let opts: [ICDownloadOption: Any] = [
            .downloadsDirectoryURL: outDir,
            .overwrite: false,
        ]
        camera?.requestDownloadFile(
            file, options: opts, downloadDelegate: self,
            didDownloadSelector: #selector(didDownload(_:error:options:contextInfo:)),
            contextInfo: nil)
    }

    @objc func didDownload(_ file: ICCameraFile, error: Error?, options: [AnyHashable: Any]?, contextInfo: UnsafeMutableRawPointer?) {
        if let error = error {
            emit(["event": "error", "msg": "unduh \(file.name ?? "?") gagal: \(error.localizedDescription)"])
            return
        }
        // Nama berkas hasil unduh bisa berbeda bila ada bentrok nama.
        let name = (options?[ICDownloadOption.savedFilename.rawValue] as? String) ?? file.name ?? ""
        let path = outDir.appendingPathComponent(name).path
        emit(["event": "file", "path": path])
    }
}

// MARK: - menemukan kamera

extension CameraHelper: ICDeviceBrowserDelegate {
    func deviceBrowser(_ browser: ICDeviceBrowser, didAdd device: ICDevice, moreComing: Bool) {
        guard let cam = device as? ICCameraDevice else { return }
        // Kartu memori / card reader / kamera mode "flashdisk" muncul sebagai mass storage.
        // Itu ditangani aplikasi sebagai DRIVE (folder DCIM), bukan lewat jalur tether ini —
        // kalau tidak, satu perangkat diurus dua jalur dan statusnya jadi membingungkan.
        if cam.transportType == ICDeviceTransport.transportTypeMassStorage.rawValue {
            emit(["event": "skip", "reason": "mass-storage", "model": cam.name ?? ""])
            return
        }
        camera = cam
        cam.delegate = self
        baseline.removeAll()
        baselineReady = false
        cam.requestOpenSession()
        emit(["event": "connected", "model": cam.name ?? "kamera"])
    }

    func deviceBrowser(_ browser: ICDeviceBrowser, didRemove device: ICDevice, moreGoing: Bool) {
        guard device === camera else { return }
        camera = nil
        baseline.removeAll()
        baselineReady = false
        emit(["event": "disconnected"])
    }
}

// MARK: - isi kamera

extension CameraHelper: ICCameraDeviceDelegate {
    // Enumerasi isi kamera selesai → semua yang ADA SEKARANG dijadikan patokan (diabaikan).
    func deviceDidBecomeReady(withCompleteContentCatalog device: ICCameraDevice) {
        for item in device.contents ?? [] {
            if let f = item as? ICCameraFile, let n = f.name { baseline.insert(n) }
        }
        baselineReady = true
        emit(["event": "catalog", "ignored": baseline.count])
    }

    // Jepretan baru masuk.
    func cameraDevice(_ camera: ICCameraDevice, didAdd items: [ICCameraItem]) {
        for item in items {
            guard let f = item as? ICCameraFile, let name = f.name else { continue }
            if !baselineReady || baseline.contains(name) { continue } // foto lama
            baseline.insert(name)
            download(f)
        }
    }

    func cameraDevice(_ camera: ICCameraDevice, didRemove items: [ICCameraItem]) {}
    func cameraDevice(_ camera: ICCameraDevice, didRenameItems items: [ICCameraItem]) {}
    func cameraDevice(_ camera: ICCameraDevice, didReceiveThumbnail thumbnail: CGImage?, for item: ICCameraItem, error: Error?) {}
    func cameraDevice(_ camera: ICCameraDevice, didReceiveMetadata metadata: [AnyHashable: Any]?, for item: ICCameraItem, error: Error?) {}
    func cameraDeviceDidChangeCapability(_ camera: ICCameraDevice) {}
    func cameraDevice(_ camera: ICCameraDevice, didReceivePTPEvent eventData: Data) {}
    func cameraDeviceDidRemoveAccessRestriction(_ device: ICDevice) {}
    func cameraDeviceDidEnableAccessRestriction(_ device: ICDevice) {}

    // --- ICDeviceDelegate (wajib) ---
    func device(_ device: ICDevice, didOpenSessionWithError error: Error?) {
        if let error = error {
            emit(["event": "error", "msg": "buka sesi kamera gagal: \(error.localizedDescription)"])
        }
    }
    func device(_ device: ICDevice, didCloseSessionWithError error: Error?) {}
    func didRemove(_ device: ICDevice) {
        if device === camera {
            camera = nil
            baselineReady = false
            emit(["event": "disconnected"])
        }
    }
    func device(_ device: ICDevice, didEncounterError error: Error?) {
        if let error = error { emit(["event": "error", "msg": error.localizedDescription]) }
    }
}

extension CameraHelper: ICCameraDeviceDownloadDelegate {}

// MARK: - main

let args = CommandLine.arguments
guard args.count >= 2 else {
    emit(["event": "error", "msg": "pemakaian: milesshoot-kamera <folder-tujuan>"])
    exit(2)
}
let helper = CameraHelper(outDir: URL(fileURLWithPath: args[1], isDirectory: true))
helper.start()
RunLoop.main.run()
