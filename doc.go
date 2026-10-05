// Package pdf provides a PDF viewer widget for Fyne, written in pure Go.
//
// [Document] is a ready-to-use viewer with a thumbnail strip and zoom
// controls. [Source] gives goroutine-safe low-level access to an opened PDF
// (page count, bounds, rendering, metadata), and [NewImageFromMemory] renders
// a single page without any UI.
//
// Page indices are 0-based throughout the package.
package pdf
