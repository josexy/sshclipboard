// Package capture turns clipboard changes into captured items: it
// debounces bursts, classifies content as files, image, or text, filters
// folders, confidential content, and the app's own writes, stages images and
// text to disk with hashes and thumbnails, and suppresses duplicates.
package capture
