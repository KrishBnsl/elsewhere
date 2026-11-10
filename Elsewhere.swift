import Accelerate
import AVFoundation
import CoreAudio
import Darwin
import Foundation
import ImageIO

struct Track {
    let url: URL
    let title: String
    let artist: String
    let album: String
}

struct Album {
    let title: String
    let tracks: [Track]
    let artwork: [[String]]?

    var artist: String {
        let artists = Set(tracks.map(\.artist))
        return artists.count == 1 ? tracks[0].artist : "Various artists"
    }
}

enum Cover {
    static let size = 16
    static let reset = "\u{1b}[48;5;235m"

    static func pixels(_ source: CGImageSource?) -> [[String]]? {
        guard let source, let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: 64
        ] as CFDictionary) else { return nil }

        var bytes = [UInt8](repeating: 0, count: size * size * 4)
        let rendered = bytes.withUnsafeMutableBytes { buffer -> Bool in
            guard let context = CGContext(data: buffer.baseAddress, width: size, height: size,
                                          bitsPerComponent: 8, bytesPerRow: size * 4,
                                          space: CGColorSpaceCreateDeviceRGB(),
                                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return false }
            context.interpolationQuality = .high
            context.draw(image, in: CGRect(x: 0, y: 0, width: size, height: size))
            return true
        }
        guard rendered else { return nil }
        return (0..<size).map { y in
            (0..<size).map { x in
                let i = (y * size + x) * 4
                return "\u{1b}[48;2;\(bytes[i]);\(bytes[i + 1]);\(bytes[i + 2])m  "
            }
        }
    }

    static func inFolder(_ folder: URL) -> [[String]]? {
        for name in ["cover", "folder", "front"] {
            for ext in ["jpg", "jpeg", "png"] {
                let url = folder.appendingPathComponent(name).appendingPathExtension(ext)
                if FileManager.default.fileExists(atPath: url.path),
                   let art = pixels(CGImageSourceCreateWithURL(url as CFURL, nil)) { return art }
            }
        }
        return nil
    }

    static let placeholder = [
        "                ", "      ████      ", "    ██░░░░██    ", "   ██░█▓▓█░██   ",
        "   ██░█▓▓█░██   ", "    ██░░░░██    ", "      ████      ", "                "
    ]
}

enum OutputVolume {
    static func read() -> Int? {
        var device = AudioDeviceID(kAudioObjectUnknown)
        guard readProperty(AudioObjectID(kAudioObjectSystemObject), kAudioHardwarePropertyDefaultOutputDevice,
                           kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain, into: &device),
              device != kAudioObjectUnknown else { return nil }

        let scope = kAudioDevicePropertyScopeOutput
        var muted: UInt32 = 0
        if readProperty(device, kAudioDevicePropertyMute, scope, kAudioObjectPropertyElementMain, into: &muted),
           muted != 0 { return 0 }

        var volume: Float32 = 0
        if readProperty(device, kAudioDevicePropertyVolumeScalar, scope, kAudioObjectPropertyElementMain, into: &volume) {
            return percent(volume)
        }
        let channels = [1, 2].compactMap { channel -> Float32? in
            var value: Float32 = 0
            return readProperty(device, kAudioDevicePropertyVolumeScalar, scope,
                                AudioObjectPropertyElement(channel), into: &value) ? value : nil
        }
        return channels.isEmpty ? nil : percent(channels.reduce(0, +) / Float32(channels.count))
    }

    private static func readProperty<T>(_ object: AudioObjectID, _ selector: AudioObjectPropertySelector,
                                        _ scope: AudioObjectPropertyScope, _ element: AudioObjectPropertyElement,
                                        into value: inout T) -> Bool {
        var address = AudioObjectPropertyAddress(mSelector: selector, mScope: scope, mElement: element)
        var size = UInt32(MemoryLayout<T>.size)
        return withUnsafeMutableBytes(of: &value) { buffer in
            AudioObjectGetPropertyData(object, &address, 0, nil, &size, buffer.baseAddress!) == noErr
        }
    }

    static func percent(_ scalar: Float32) -> Int {
        Int((min(1, max(0, scalar)) * 100).rounded())
    }
}

enum Library {
    static let extensions: Set<String> = ["mp3", "m4a", "aac", "flac", "wav", "aif", "aiff", "caf", "alac"]

    static func scan(_ root: URL) async throws -> [Album] {
        var directory: ObjCBool = false
        guard FileManager.default.fileExists(atPath: root.path, isDirectory: &directory), directory.boolValue else {
            throw CocoaError(.fileReadNoSuchFile, userInfo: [NSFilePathErrorKey: root.path])
        }
        let keys: Set<URLResourceKey> = [.isRegularFileKey]
        guard let files = FileManager.default.enumerator(at: root, includingPropertiesForKeys: Array(keys), options: [.skipsHiddenFiles, .skipsPackageDescendants]) else {
            throw CocoaError(.fileReadNoPermission, userInfo: [NSFilePathErrorKey: root.path])
        }
        let urls = files.allObjects.compactMap { $0 as? URL }.filter {
            extensions.contains($0.pathExtension.lowercased()) && (try? $0.resourceValues(forKeys: keys).isRegularFile) == true
        }.sorted { $0.path.localizedStandardCompare($1.path) == .orderedAscending }
        var groups: [URL: [String: [Track]]] = [:]
        var artwork: [URL: [String: [[String]]]] = [:]
        for url in urls {
            let folder = url.deletingLastPathComponent()
            let asset = AVURLAsset(url: url)
            let metadata = (try? await asset.load(.commonMetadata)) ?? []
            var tags: [AVMetadataKey: String] = [:]
            for item in metadata {
                guard let key = item.commonKey,
                      [.commonKeyTitle, .commonKeyArtist, .commonKeyAlbumName].contains(key),
                      let value = try? await item.load(.stringValue),
                      !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { continue }
                tags[key] = value
            }
            let album = tags[.commonKeyAlbumName] ?? folder.lastPathComponent
            let track = Track(url: url, title: tags[.commonKeyTitle] ?? url.deletingPathExtension().lastPathComponent,
                              artist: tags[.commonKeyArtist] ?? "Unknown artist", album: album)
            if artwork[folder]?[album] == nil {
                if groups[folder]?[album] == nil, let cover = Cover.inFolder(folder) {
                    artwork[folder, default: [:]][album] = cover
                } else if let item = metadata.first(where: { $0.commonKey == .commonKeyArtwork }),
                          let data = try? await item.load(.dataValue),
                          let cover = Cover.pixels(CGImageSourceCreateWithData(data as CFData, nil)) {
                    artwork[folder, default: [:]][album] = cover
                }
            }
            groups[folder, default: [:]][album, default: []].append(track)
        }
        return groups.flatMap { folder, group in
            group.map { Album(title: $0.key, tracks: $0.value, artwork: artwork[folder]?[$0.key]) }
        }.sorted {
            let order = $0.title.localizedStandardCompare($1.title)
            return order == .orderedSame ? $0.tracks[0].url.path < $1.tracks[0].url.path : order == .orderedAscending
        }
    }
}

func clock(_ seconds: TimeInterval) -> String {
    let whole = Int(seconds)
    return String(format: "%d:%02d", whole / 60, whole % 60)
}

func cell(_ text: String, _ width: Int) -> String {
    // Use the terminal's column widths for CJK text and combining characters.
    var output = ""
    var used = 0
    for character in text {
        let widths = character.unicodeScalars.map { Int(wcwidth(Int32($0.value))) }
        let printable = widths.allSatisfy { $0 >= 0 } && !character.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains)
        let displayWidth = printable ? widths.reduce(0, +) : 1
        guard used + displayWidth <= width else { break }
        output.append(printable ? character : "?")
        used += displayWidth
    }
    return output + String(repeating: " ", count: max(0, width - used))
}

final class Spectrum {
    private static let sampleCount = 2048
    private static let bands = 24
    private static let window = (0..<sampleCount).map {
        Float(0.5 - 0.5 * cos(2 * Double.pi * Double($0) / Double(sampleCount - 1)))
    }
    private let file: AVAudioFile
    private let buffer: AVAudioPCMBuffer
    private let transform: vDSP_DFT_Setup
    private let bins: [Range<Int>]
    private let zeros = [Float](repeating: 0, count: sampleCount)
    private var real = [Float](repeating: 0, count: sampleCount)
    private var imaginary = [Float](repeating: 0, count: sampleCount)
    private var input = [Float](repeating: 0, count: sampleCount)
    private(set) var levels = [Float](repeating: 0, count: bands)

    init?(url: URL) {
        guard let file = try? AVAudioFile(forReading: url, commonFormat: .pcmFormatFloat32, interleaved: false),
              file.processingFormat.sampleRate > 90,
              let buffer = AVAudioPCMBuffer(pcmFormat: file.processingFormat, frameCapacity: UInt32(Self.sampleCount)),
              let transform = vDSP_DFT_zop_CreateSetup(nil, vDSP_Length(Self.sampleCount), .FORWARD) else { return nil }
        self.file = file
        self.buffer = buffer
        self.transform = transform
        let sampleRate = file.processingFormat.sampleRate
        let nyquist = min(sampleRate / 2, 16_000)
        let scale = Double(Self.sampleCount) / sampleRate
        bins = (0..<Self.bands).map { band in
            let low = 45 * pow(nyquist / 45, Double(band) / Double(Self.bands))
            let high = 45 * pow(nyquist / 45, Double(band + 1) / Double(Self.bands))
            let first = min(Self.sampleCount / 2 - 1, max(1, Int(ceil(low * scale))))
            let end = min(Self.sampleCount / 2, max(first + 1, Int(ceil(high * scale))))
            return first..<end
        }
    }

    deinit { vDSP_DFT_DestroySetup(transform) }

    func sample(at seconds: TimeInterval) {
        let sampleRate = file.processingFormat.sampleRate
        let offset = AVAudioFramePosition(max(0, seconds) * sampleRate)
        guard offset < file.length else { clear(); return }
        file.framePosition = offset
        do {
            try file.read(into: buffer)
        } catch {
            clear()
            return
        }
        guard let channels = buffer.floatChannelData else { clear(); return }

        var peaks = [Float](repeating: 0, count: Self.bands)
        let count = Int(buffer.frameLength)
        for channel in 0..<min(2, Int(file.processingFormat.channelCount)) {
            for i in 0..<Self.sampleCount {
                input[i] = i < count ? channels[channel][i] * Self.window[i] : 0
            }
            vDSP_DFT_Execute(transform, input, zeros, &real, &imaginary)
            for band in peaks.indices {
                for bin in bins[band] {
                    peaks[band] = max(peaks[band], real[bin] * real[bin] + imaginary[bin] * imaginary[bin])
                }
            }
        }

        for band in levels.indices {
            let amplitude = sqrt(peaks[band]) * 4 / Float(Self.sampleCount)
            let decibels = 20 * log10(max(amplitude, 0.000001))
            let height = min(1, max(0, (decibels + 54) / 49))
            levels[band] = height >= levels[band] ? height : max(height, levels[band] * 0.77)
        }
    }

    func clear() {
        levels = [Float](repeating: 0, count: Self.bands)
    }

    func rows(width: Int, height: Int, phase: Int) -> [String] {
        let glyphs = Array("01+.:/|")
        let blocks = Array(" ▁▂▃▄▅▆▇█")
        let bandCount = min(levels.count, max(1, width / 2))
        return (0..<height).map { row in
            var line = ""
            var previousColor = -1
            for x in 0..<width {
                let band = x * bandCount / width
                let first = band * levels.count / bandCount
                let end = (band + 1) * levels.count / bandCount
                let level = levels[first..<end].max() ?? 0
                let steps = Int((level * Float(height * 8)).rounded())
                let fill = min(8, max(0, steps - (height - row - 1) * 8))
                let period = height + 9 + x % 5
                let head = (phase / (2 + x % 3) + x * 13) % period
                let trail = (head - row + period) % period
                let glyph: Character
                let color: Int
                if fill > 0, x == 0 || band != (x - 1) * bandCount / width {
                    glyph = blocks[fill]
                    color = steps <= (height - row) * 8 ? 221 : 178
                } else {
                    glyph = trail < 5 ? glyphs[(x * 7 + row * 3 + head) % glyphs.count] : " "
                    color = trail == 0 ? 136 : 94
                }
                if glyph != " ", color != previousColor {
                    line += "\u{1b}[38;5;\(color)m"
                    previousColor = color
                }
                line.append(glyph)
            }
            return line + "\u{1b}[38;5;221m"
        }
    }
}

final class Player {
    let albums: [Album]
    let path: String
    var results: [Album]
    var albumIndex = 0
    var trackIndex = 0
    var trackPane = false
    var searching = false
    var query = ""
    var notice = ""
    var volume: Float = 0.7
    var outputVolume: Int?
    var audio: AVAudioPlayer?
    var queue: [Track] = []
    var playingIndex = 0
    var expectedPlayback = false
    var showNowPlaying = false
    var showSpectrum = true
    private var spectrum: Spectrum?
    private var spectrumURL: URL?
    private var nextSpectrumFrame: TimeInterval = 0
    private var spectrumPhase = 0

    var animateSpectrum: Bool {
        showNowPlaying && showSpectrum && audio?.isPlaying == true
    }

    func updateSpectrum() {
        guard showNowPlaying, showSpectrum, let audio, let url = current?.url else { return }
        if spectrumURL != url {
            spectrumURL = url
            spectrum = Spectrum(url: url)
            nextSpectrumFrame = 0
        }
        guard audio.isPlaying else { spectrum?.clear(); return }
        let now = ProcessInfo.processInfo.systemUptime
        guard now >= nextSpectrumFrame else { return }
        nextSpectrumFrame = now + 0.075
        spectrum?.sample(at: audio.currentTime)
        spectrumPhase = (spectrumPhase + 1) % 100_000
    }
    var playingArtwork: [[String]]?

    init(albums: [Album], path: String) {
        self.albums = albums
        self.results = albums
        self.path = path
    }

    var tracks: [Track] { results.indices.contains(albumIndex) ? results[albumIndex].tracks : [] }
    var current: Track? { queue.indices.contains(playingIndex) ? queue[playingIndex] : nil }

    func search() {
        results = query.isEmpty ? albums : albums.filter { album in
            album.title.localizedCaseInsensitiveContains(query) ||
            album.tracks.contains { $0.title.localizedCaseInsensitiveContains(query) || $0.artist.localizedCaseInsensitiveContains(query) }
        }
        albumIndex = 0
        trackIndex = 0
    }

    func playSelection() {
        guard results.indices.contains(albumIndex) else { return }
        let album = results[albumIndex]
        if start(album.tracks, at: trackPane ? trackIndex : 0) {
            playingArtwork = album.artwork
            showNowPlaying = true
        }
    }

    @discardableResult
    func start(_ tracks: [Track], at index: Int) -> Bool {
        guard tracks.indices.contains(index) else { return false }
        let track = tracks[index]
        do {
            let next = try AVAudioPlayer(contentsOf: track.url)
            next.volume = volume
            guard next.play() else { throw CocoaError(.fileReadUnknown) }
            audio?.stop()
            audio = next
            queue = tracks
            playingIndex = index
            expectedPlayback = true
            notice = ""
            return true
        } catch {
            notice = "Cannot play \(track.title): \(error.localizedDescription)"
            return false
        }
    }

    func tick() {
        guard expectedPlayback, audio?.isPlaying == false else { return }
        expectedPlayback = false
        if queue.indices.contains(playingIndex + 1) {
            start(queue, at: playingIndex + 1)
        } else {
            audio?.currentTime = audio?.duration ?? 0
        }
    }

    func handle(_ key: String) -> Bool {
        if key == "\u{3}" { return false }
        if searching {
            switch key {
            case "\r", "\n": searching = false
            case "\u{1b}": searching = false; query = ""; search()
            case "\u{7f}", "\u{8}":
                if !query.isEmpty { query.removeLast(); search() }
            default:
                if !key.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains) {
                    query += key
                    search()
                }
            }
            return true
        }
        if showNowPlaying {
            switch key {
            case "\u{1b}", "m": showNowPlaying = false; return true
            case "v": showSpectrum.toggle(); return true
            case "q", " ", "n", "p", "[", "]", "-", "+", "=": break
            default: return true
            }
        }
        switch key {
        case "m": showNowPlaying = current != nil
        case "q": return false
        case "/", "f": searching = true
        case "\t": trackPane.toggle()
        case "\u{1b}[C", "l": trackPane = true
        case "\u{1b}[D", "h": trackPane = false
        case "\u{1b}[A", "k": move(-1)
        case "\u{1b}[B", "j": move(1)
        case "\r", "\n": playSelection()
        case " ":
            if let audio {
                if audio.isPlaying {
                    audio.pause()
                    expectedPlayback = false
                } else {
                    if audio.currentTime >= audio.duration { audio.currentTime = 0 }
                    expectedPlayback = audio.play()
                    if !expectedPlayback { notice = "Cannot resume playback" }
                }
            } else {
                playSelection()
            }
        case "n": start(queue, at: playingIndex + 1)
        case "p": start(queue, at: playingIndex - 1)
        case "[", "]":
            if let audio {
                let target = audio.currentTime + (key == "]" ? 5 : -5)
                audio.currentTime = min(max(0, audio.duration - 0.01), max(0, target))
            }
        case "-", "+", "=":
            volume = max(0, min(1, volume + (key == "-" ? -0.05 : 0.05)))
            audio?.volume = volume
        default: break
        }
        return true
    }

    func move(_ offset: Int) {
        if trackPane {
            trackIndex = min(max(0, tracks.count - 1), max(0, trackIndex + offset))
        } else {
            albumIndex = min(max(0, results.count - 1), max(0, albumIndex + offset))
            trackIndex = 0
        }
    }

    func frame(width: Int, height: Int) -> String {
        let amber = "\u{1b}[38;5;221m"
        let dim = "\u{1b}[38;5;179m"
        let black = "\u{1b}[48;5;232m"
        let selected = "\u{1b}[38;5;232;48;5;214m"
        let reset = "\u{1b}[0m"
        func row(_ value: String, _ style: String = "") -> String {
            style + cell(value, width) + reset
        }

        guard width >= 45, height >= 13 else {
            return row(" Enlarge terminal (45 x 13). q quits", amber) + "\u{1b}[J"
        }
        let playing = audio?.isPlaying == true
        let state = playing ? "PLAY" : (audio == nil ? "STOP" : "PAUSE")
        let elapsed = audio?.currentTime ?? 0
        let duration = audio?.duration ?? 0
        let barWidth = max(8, min(42, width - 35))
        let filled = duration > 0 ? Int(min(1, elapsed / duration) * Double(barWidth)) : 0
        let progress = String(repeating: "━", count: filled) + String(repeating: "─", count: barWidth - filled)
        let transport = " \(state)  \(clock(elapsed)) \(progress) \(clock(duration))   VOL \(OutputVolume.percent(volume))%"
        let leftWidth = max(15, width * 34 / 100)
        let rightWidth = width - leftWidth - 1
        let bodyHeight = height - 9
        let albumStart = max(0, min(albumIndex - bodyHeight / 2, results.count - bodyHeight))
        let trackStart = max(0, min(trackIndex - bodyHeight / 2, tracks.count - bodyHeight))
        let title = " ELSEWHERE / LOCAL AUDIO"
        let badge = "STEREO "
        var rows = [
            row(cell(title, width - badge.count) + badge, selected),
            row(" ── NOW PLAYING " + String(repeating: "─", count: width), dim),
            row(current.map { " \($0.title)  /  \($0.artist)  /  \($0.album)" } ?? " Choose an album or track and press Enter", amber),
            row(transport, amber),
            dim + cell(trackPane ? "  ALBUMS" : " › ALBUMS", leftWidth) + "│" +
                cell(trackPane ? " › TRACKS" : "  TRACKS", rightWidth) + reset
        ]
        for line in 0..<bodyHeight {
            let a = albumStart + line
            let t = trackStart + line
            let album = results.indices.contains(a) ? " \(results[a].title)" : ""
            let marker = tracks.indices.contains(t) && tracks[t].url == current?.url ? "▶" : " "
            let track = tracks.indices.contains(t) ? " \(marker) \(String(format: "%02d", t + 1))  \(tracks[t].title)" : ""
            let leftStyle = a == albumIndex && !album.isEmpty ? (trackPane ? amber : selected) : dim
            let rightStyle = t == trackIndex && !track.isEmpty && trackPane ? selected : amber
            rows.append(leftStyle + cell(album, leftWidth) + black + dim + "│" + rightStyle + cell(track, rightWidth) + reset)
        }
        let detail = tracks.indices.contains(trackIndex) ? " \(tracks[trackIndex].artist)  /  \(tracks[trackIndex].url.pathExtension.uppercased())" : " \(path)"
        rows.append(row(notice.isEmpty ? detail : " " + notice, dim))
        rows.append(row(searching || !query.isEmpty ? " / \(query)\(searching ? "_" : "")  ·  \(results.count) albums" : " \(results.count) albums  ·  \(path)", amber))
        rows.append(row(" ↑↓ Move  Tab Pane  Enter Play  Space Pause  / Find", selected))
        rows.append(row(" N/P Skip  [/] Seek  +/- Vol  M Player  Q Quit", selected))
        let library = rows.joined(separator: "\r\n") + "\u{1b}[J"
        guard showNowPlaying, let current else { return library }
        return library + popup(current, width: width, height: height, state: state, elapsed: elapsed, duration: duration)
    }

    func popup(_ track: Track, width: Int, height: Int, state: String, elapsed: TimeInterval, duration: TimeInterval) -> String {
        let columns = min(112, width - max(2, width / 10))
        let artSize = min(Cover.size, max(8, (columns - 36) / 3), height - 5)
        let infoWidth = columns - 2 * artSize - 6
        let x = (width - columns) / 2 + 1
        let padding = height >= artSize + 7 ? 1 : 0
        let y = (height - artSize - 4 - 2 * padding) / 2 + 1
        let panel = Cover.reset + "\u{1b}[38;5;221m"
        let reset = "\u{1b}[0m"
        let placeholder = Cover.placeholder.map(Array.init)
        let outputText = outputVolume.map { "  OUT \($0)%" } ?? ""
        let progressWidth = infoWidth - 2
        let played = duration > 0 ? Int(min(1, elapsed / duration) * Double(progressWidth)) : 0
        let progress = String(repeating: "━", count: played) + String(repeating: "─", count: progressWidth - played)
        let info = [
            track.title, track.artist, track.album, "",
            progress,
            "\(state)  \(clock(elapsed)) / \(clock(duration))",
            "APP \(OutputVolume.percent(volume))%" + outputText,
            notice.isEmpty ? "TRACK \(playingIndex + 1)/\(queue.count)  \(track.url.pathExtension.uppercased())" : notice
        ]
        let chartHeight = max(1, artSize - info.count - 1)
        let chart = showSpectrum ? spectrum?.rows(width: infoWidth, height: chartHeight, phase: spectrumPhase) : nil
        let heading = "─ NOW PLAYING "
        var lines = ["┌" + heading + String(repeating: "─", count: columns - heading.count - 2) + "┐"]
        let blank = "│" + String(repeating: " ", count: columns - 2) + "│"
        if padding > 0 { lines.append(blank) }
        for i in 0..<artSize {
            let artRow: String
            if let artwork = playingArtwork {
                let pixels = artwork[i * Cover.size / artSize]
                artRow = (0..<artSize).map { pixels[$0 * Cover.size / artSize] }.joined() + Cover.reset
            } else {
                let pixels = placeholder[i * placeholder.count / artSize]
                artRow = String((0..<(artSize * 2)).map { pixels[$0 * pixels.count / (artSize * 2)] })
            }
            let text: String
            if let chart, chartHeight == 1, i == 3 {
                text = chart[0]
            } else if let chart, chartHeight > 1, i >= info.count {
                let row = i - info.count
                text = row == 0 ? cell("─ SPECTRUM / AMBER RAIN", infoWidth) : chart[row - 1]
            } else {
                text = cell(i < info.count ? info[i] : "", infoWidth)
            }
            lines.append("│ " + artRow + "  " + text + " │")
        }
        if padding > 0 { lines.append(blank) }
        lines.append("│" + cell(" Space Play/Pause  N/P Skip  [/] Seek", columns - 2) + "│")
        lines.append("│" + cell(" Esc Library  +/- Vol  V Visuals " + (showSpectrum ? "on" : "off"), columns - 2) + "│")
        lines.append("└" + String(repeating: "─", count: columns - 2) + "┘")
        return lines.enumerated().map { offset, line in
            "\u{1b}[\(y + offset);\(x)H" + panel + line + reset
        }.joined()
    }
}

func runTerminal(_ player: Player) throws {
    guard isatty(STDIN_FILENO) == 1, isatty(STDOUT_FILENO) == 1 else { throw CocoaError(.featureUnsupported) }
    var original = termios()
    guard tcgetattr(STDIN_FILENO, &original) == 0 else { throw POSIXError(.EIO) }
    var raw = original
    cfmakeraw(&raw)
    guard tcsetattr(STDIN_FILENO, TCSAFLUSH, &raw) == 0 else { throw POSIXError(.EIO) }

    let output = FileHandle.standardOutput
    func write(_ string: String) throws { try output.write(contentsOf: Data(string.utf8)) }
    defer {
        _ = tcsetattr(STDIN_FILENO, TCSAFLUSH, &original)
        try? write("\u{1b}[0m\u{1b}[?25h\u{1b}[?1049l")
    }
    try write("\u{1b}[?1049h\u{1b}[?25l\u{1b}[2J")
    var previous = ""
    var bytes = [UInt8](repeating: 0, count: 256)
    var pending: [UInt8] = []
    var input = pollfd(fd: STDIN_FILENO, events: Int16(POLLIN), revents: 0)
    while true {
        player.tick()
        player.updateSpectrum()
        if player.showNowPlaying { player.outputVolume = OutputVolume.read() }
        var size = winsize()
        _ = ioctl(STDOUT_FILENO, TIOCGWINSZ, &size)
        let frame = player.frame(width: size.ws_col > 0 ? Int(size.ws_col) : 80,
                                 height: size.ws_row > 0 ? Int(size.ws_row) : 24)
        if frame != previous {
            try write("\u{1b}[H" + frame)
            previous = frame
        }
        let ready = poll(&input, 1, pending.isEmpty ? (player.animateSpectrum ? 75 : 150) : 35)
        if ready < 0 {
            if errno == EINTR { continue }
            throw POSIXError(.EIO)
        }
        if ready > 0 {
            let count = read(STDIN_FILENO, &bytes, bytes.count)
            guard count > 0 else { return }
            pending += bytes.prefix(count)
        }
        while let first = pending.first {
            var count = 1
            if first == 27 {
                if pending.count < 2 && ready > 0 { break }
                if pending.count >= 2 && (pending[1] == 91 || pending[1] == 79) {
                    if let end = pending.indices.dropFirst(2).first(where: { (64...126).contains(pending[$0]) }) {
                        count = end + 1
                        if pending[1] == 79 { pending[1] = 91 }
                    } else if ready > 0 { break }
                    else { count = pending.count }
                }
            } else if first >= 0xC2 {
                count = first < 0xE0 ? 2 : (first < 0xF0 ? 3 : 4)
                if count > pending.count && ready > 0 { break }
                count = min(count, pending.count)
            }
            let key = String(decoding: pending.prefix(count), as: UTF8.self)
            pending.removeFirst(count)
            if !player.handle(key) { return }
        }
    }
}

_ = setlocale(LC_CTYPE, "")
let arguments = Array(CommandLine.arguments.dropFirst())
if arguments == ["--help"] {
    print("Usage: swift Elsewhere.swift [music-folder]")
} else if arguments.count > 1 {
    FileHandle.standardError.write(Data("Usage: swift Elsewhere.swift [music-folder]\n".utf8))
    exit(2)
} else {
    let path = arguments.first ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Music").path
    let folder = URL(fileURLWithPath: (path as NSString).expandingTildeInPath).standardizedFileURL
    do {
        let albums = try await Library.scan(folder)
        try runTerminal(Player(albums: albums, path: folder.path))
    } catch {
        FileHandle.standardError.write(Data("Elsewhere: \(error.localizedDescription)\n".utf8))
        exit(1)
    }
}
