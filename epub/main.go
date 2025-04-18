package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	epub "github.com/go-shiori/go-epub"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/frontmatter"
)

func main() {
	// Create a new EPUB book
	book, err := epub.NewEpub("Il Libro Open Source")
	if err != nil {
		panic(err)
	}

	// Set the title and author
	book.SetTitle("Il manuale del buon dev")
	book.SetAuthor("Community")

	coverPath, err := book.AddImage("./cover.jpg", "cover.jpg")
	if err != nil {
		panic(err)
	}

	cssPath, err := book.AddCSS("./style.css", "style.css")
	if err != nil {
		panic(err)
	}

	book.SetCover(coverPath, "")

	//uid := "https://il-libro-open-source.github.io/book/"
	//book.SetIdentifier(uid)
	uid := "f9298b0f-bea1-4cb6-a601-2a35027bd44e"
	book.SetIdentifier("urn:uuid:" + uid)
	book.SetLang("it")

	mdPath := "../docs/it"

	cv := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			&frontmatter.Extender{},
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(
					chromahtml.WithLineNumbers(true),
					chromahtml.WrapLongLines(true),
					chromahtml.TabWidth(2),
				),
			),
		),
		goldmark.WithRendererOptions(
			html.WithXHTML(),
			html.WithUnsafe(),
		),
	)

	chapters, err := getChapters(&cv, mdPath)
	if err != nil {
		panic(err)
	}

	actPath, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	err = addImages(book, chapters, actPath)
	if err != nil {
		panic(err)
	}

	err = createChapters(book, chapters, cssPath, "")
	if err != nil {
		panic(err)
	}

	// Save the EPUB book
	err = book.Write("./il-manuale-del-buon-dev.epub")
	if err != nil {
		panic(err)
	}
}

func addImages(book *epub.Epub, chapters []*Chapter, actPath string) (err error) {
	passedImages := map[string]string{}
	for _, chapter := range chapters {
		for _, image := range chapter.Images {
			intPath, ok := passedImages[image]
			if !ok {
				fsPath := strings.Replace(image, "/book", actPath+"/..", 1)
				fileName := strings.ReplaceAll(strings.TrimLeft(image, "/"), "/", "_")
				intPath, err = book.AddImage(fsPath, fileName)
				if err != nil {
					err = fmt.Errorf("failed to add image %s: %w", fsPath, err)
					return
				}
				passedImages[image] = intPath
			}

			// Replace the image path in the HTML content
			chapter.Html = strings.ReplaceAll(chapter.Html, image, intPath)
		}

		if len(chapter.Children) > 0 {
			err = addImages(book, chapter.Children, actPath)
			if err != nil {
				return
			}
		}
	}
	return nil
}

func createChapters(book *epub.Epub, chapters []*Chapter, cssPath string, parent string) error {
	for _, chapter := range chapters {
		// Add a chapter
		baseName := ""
		if parent != "" {
			// Add a sub-chapter
			baseName = parent + "__"
		}
		baseName += chapter.Filename

		fileName := baseName + ".xhtml"

		var err error
		if parent != "" {
			// Add a sub-chapter
			_, err = book.AddSubSection(parent, chapter.Html, chapter.Meta.Title, fileName, cssPath)
		} else {
			_, err = book.AddSection(chapter.Html, chapter.Meta.Title, fileName, cssPath)
		}

		if err != nil {
			return err
		}

		// Recursively add child chapters
		if len(chapter.Children) > 0 {
			err := createChapters(book, chapter.Children, cssPath, fileName)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

type Chapter struct {
	Filename string
	Meta     ChapterMeta
	Content  string
	Html     string
	Children []*Chapter
	Images   []string
}

type ChapterMeta struct {
	Title string `yaml:"title"`
	Order int    `yaml:"nav_order"`
}

func getChapters(cv *goldmark.Markdown, mdPath string) (list []*Chapter, err error) {
	// Read markdown files from the directory
	files, err := fs.ReadDir(os.DirFS(mdPath), ".")
	if err != nil {
		return
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".md" {
			content, err := os.ReadFile(filepath.Join(mdPath, file.Name()))
			if err != nil {
				panic(err)
			}

			// Remove Jekyll-specific codes
			tempContent := string(content)
			tempContent = regexp.MustCompile(`\{:[^}]*\}`).ReplaceAllString(tempContent, "")
			tempContent = strings.ReplaceAll(tempContent, "- TOC", "")
			content = []byte(tempContent)

			// Find all images paths
			images := []string{}
			re := regexp.MustCompile(`!\[.*?\]\((.*?)\)`)
			matches := re.FindAllSubmatch(content, -1)
			for _, match := range matches {
				if len(match) > 1 {
					images = append(images, string(match[1]))
				}
			}

			ctx := parser.NewContext()
			var buf bytes.Buffer
			if err := (*cv).Convert(content, &buf, parser.WithContext(ctx)); err != nil {
				panic(err)
			}

			d := frontmatter.Get(ctx)

			meta := ChapterMeta{}
			err = d.Decode(&meta)
			if err != nil {
				return nil, err
			}

			html := buf.String()
			html = strings.ReplaceAll(html, "<br>", "<br/>")

			fileName := strings.TrimSuffix(file.Name(), ".md")

			ch := Chapter{
				Filename: fileName,
				Meta:     meta,
				Content:  string(content),
				Html:     html,
				Images:   images,
			}

			// Add children chapters if any
			dirPath := filepath.Join(mdPath, file.Name()[:len(file.Name())-3])
			if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
				children, err := getChapters(cv, dirPath)
				if err != nil {
					return nil, err
				}
				ch.Children = children
			}

			list = append(list, &ch)
		}
	}

	slices.SortFunc(list, func(a, b *Chapter) int {
		return a.Meta.Order - b.Meta.Order
	})

	return list, nil
}
