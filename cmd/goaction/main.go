// Creates action files for Go code
package main

import (
	"context"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
	"github.com/posener/goaction"
	"github.com/posener/goaction/actionutil"
	"github.com/posener/goaction/internal/metadata"
	"github.com/posener/script"
)

var (
	path  = flag.String("path", "", "Path to main Go file, this file should contain all defined flags and environment variables.")
	name  = flag.String("name", "", "Override action name, the default name is the package name.")
	desc  = flag.String("desc", "", "Override action description, the default description is the package synopsis.")
	icon  = flag.String("icon", "", "Set branding icon. Choose from https://feathericons.com.")
	color = flag.String("color", "", "Set branding color. Can be one of: white, yellow, blue, green, orange, red, purple or gray-dark")

	email       = goaction.Getenv("email", "posener@gmail.com", "Email for commit message")
	githubToken = goaction.Getenv("github-token", "", "Github token for PR comments. Optional.")
)

const (
	action     = "action.yml"
	dockerfile = "Dockerfile"
)

func init() {
	log.SetFlags(log.Lshortfile)
}

func main() {
	flag.Parse()

	// Load go code.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, *path, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	// Parse Go code to Github actions metadata.
	m, err := metadata.New(f)
	if err != nil {
		log.Fatal(err)
	}
	if *name != "" {
		m.Name = *name
	}
	if *desc != "" {
		m.Desc = *desc
	}

	m.Branding.Icon = *icon
	m.Branding.Color = *color

	// Applying changes.

	// Create action file.
	log.Printf("Writing %s\n", action)
	err = script.Writer("yml", func(w io.Writer) error { return yaml.NewEncoder(w).Encode(m) }).
		ToFile(action)
	if err != nil {
		log.Fatal(err)
	}

	// Create dockerfile
	log.Printf("Writing %s\n", dockerfile)
	dir, err := pathRelDir(*path)
	data := tmplData{Dir: dir}
	err = script.Writer("template", func(w io.Writer) error { return tmpl.Execute(w, data) }).
		ToFile(dockerfile)
	if err != nil {
		log.Fatal(err)
	}

	diff := gitDiff()

	if diff == "" {
		log.Println("No changes were made. Aborting")
		os.Exit(0)
	}

	log.Printf("Diff:\n\n%s\n\n", diff)

	// Runs only in Github CI mode.

	err = actionutil.GitConfig("goaction", email)
	if err != nil {
		log.Fatal(err)
	}

	switch {
	case goaction.IsPush():
		if !goaction.CI {
			log.Println("Skipping commit stage.")
			break
		}
		push()
	case goaction.IsPR():
		pr(diff)
	default:
		log.Println("unsupported action mode.")
	}
}

func gitDiff() string {
	var diff strings.Builder
	for _, path := range []string{action, dockerfile} {
		// Add files to git, in case it does not exists
		d, err := actionutil.GitDiff(path)
		if err != nil {
			log.Fatal(err)
		}
		if d != "" {
			diff.WriteString(fmt.Sprintf("Path `%s`:\n\n", path))
			diff.WriteString(fmt.Sprintf("```diff\n%s\n```\n\n", d))
		}
	}
	return diff.String()
}

// Commit and push chnages to upstream branch.
func push() {
	err := actionutil.GitCommitPush(
		[]string{action, dockerfile},
		"Update action files")
	if err != nil {
		log.Fatal(err)
	}
}

// Post a pull request comment with the expected diff.
func pr(diff string) {
	if githubToken == "" {
		log.Println("In order to add request comment, set the GITHUB_TOKEN input.")
		return
	}

	body := "[Goaction](https://github.com/posener/goaction) will apply the following changes after PR is merged.\n\n" + diff
	if diff == "" {
		body = "[Goaction](https://github.com/posener/goaction) detected no required changes to Github action files."
	}

	ctx := context.Background()
	err := actionutil.PRComment(ctx, githubToken, "goaction", body)
	if err != nil {
		log.Fatal(err)
	}
}

// pathRelDir returns the containing directory of a given path in a relative form, relative to the
// working directory prefixed with "./"
func pathRelDir(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	path = filepath.Dir(path)

	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	path, err = filepath.Rel(wd, path)
	if err != nil {
		return "", err
	}
	// If Rel returned ".", fix it to empty string which will eventually mutate to "./".
	if path == "." {
		path = ""
	}
	// Add a "./" prefix.
	if !strings.HasPrefix(path, "./") {
		path = "./" + path
	}
	return path, nil
}

type tmplData struct {
	Dir string
}

var tmpl = template.Must(template.New("dockerfile").Parse(`
FROM golang:1.14.1-alpine3.11
RUN apk add git

COPY . /home/src
WORKDIR /home/src
RUN go build -o /bin/action {{ .Dir }}

FROM alpine:3.11
RUN apk add git
COPY --from=0 /bin/action /bin/action

ENTRYPOINT [ "/bin/action" ]
`))
