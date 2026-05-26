// file-search-on-ocr-helper — macOS Vision OCR worker.
//
// Reads an image path from argv[1], runs VNRecognizeTextRequest with
// accurate recognition + language correction, detects the dominant
// language across the recognized text via NLLanguageRecognizer, and
// emits a single line of JSON to stdout:
//
//   {"text":"...","confidence":0.92,"language":"en-US"}
//
// Empty stdout + zero exit code = "image successfully OCRed but no
// text recognized" — the dominant case for blank screenshots /
// non-text imagery. Non-zero exit signals a programming or IO error
// (bad argv, unreadable file, Vision threw).
//
// Issue #189.

import Foundation
import Vision
import NaturalLanguage
import CoreImage

guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write("usage: file-search-on-ocr-helper <image-path>\n".data(using: .utf8)!)
    exit(64) // EX_USAGE
}

let imagePath = CommandLine.arguments[1]
let url = URL(fileURLWithPath: imagePath)

guard let image = CIImage(contentsOf: url) else {
    FileHandle.standardError.write("cannot decode image: \(imagePath)\n".data(using: .utf8)!)
    exit(66) // EX_NOINPUT
}

let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.usesLanguageCorrection = true

let handler = VNImageRequestHandler(ciImage: image, options: [:])

do {
    try handler.perform([request])
} catch {
    FileHandle.standardError.write("Vision request failed: \(error.localizedDescription)\n".data(using: .utf8)!)
    exit(70) // EX_SOFTWARE
}

guard let observations = request.results else {
    // Vision succeeded but produced no observations — emit empty result.
    let empty: [String: Any] = ["text": "", "confidence": 0.0, "language": ""]
    if let data = try? JSONSerialization.data(withJSONObject: empty) {
        FileHandle.standardOutput.write(data)
    }
    exit(0)
}

var lines: [String] = []
var confidences: [Float] = []
for obs in observations {
    if let candidate = obs.topCandidates(1).first {
        lines.append(candidate.string)
        confidences.append(candidate.confidence)
    }
}

let text = lines.joined(separator: "\n")
let avgConfidence: Double
if confidences.isEmpty {
    avgConfidence = 0.0
} else {
    let sum = confidences.reduce(Float(0), +)
    avgConfidence = Double(sum / Float(confidences.count))
}

// NLLanguageRecognizer returns BCP-47 codes like "en", "ja", "zh-Hans".
// Empty when the recognizer doesn't have enough text to decide.
var language = ""
if !text.isEmpty {
    let recognizer = NLLanguageRecognizer()
    recognizer.processString(text)
    if let dominant = recognizer.dominantLanguage {
        language = dominant.rawValue
    }
}

let result: [String: Any] = [
    "text": text,
    "confidence": avgConfidence,
    "language": language,
]

do {
    let data = try JSONSerialization.data(withJSONObject: result)
    FileHandle.standardOutput.write(data)
} catch {
    FileHandle.standardError.write("JSON encode failed: \(error.localizedDescription)\n".data(using: .utf8)!)
    exit(70) // EX_SOFTWARE
}

exit(0)
