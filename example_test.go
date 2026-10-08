package pdf_test

import (
	"fmt"
	"os"

	pdf "github.com/timzifer/fyne-pdf"
)

func ExampleOpenSource() {
	contents, err := os.ReadFile("example.pdf")
	if err != nil {
		panic(err)
	}

	src, err := pdf.OpenSource(contents)
	if err != nil {
		panic(err)
	}
	defer func() { _ = src.Close() }()

	fmt.Println("pages:", src.PageCount())
	fmt.Println("title:", src.Metadata().Title)

	img, err := src.RenderPage(0, 300)
	if err != nil {
		panic(err)
	}
	fmt.Println("size:", img.Bounds().Size())
}

func ExampleNewImageFromMemory() {
	contents, err := os.ReadFile("example.pdf")
	if err != nil {
		panic(err)
	}

	img, err := pdf.NewImageFromMemory(contents, 0)
	if err != nil {
		panic(err)
	}
	fmt.Println(img.Bounds().Size())
}
