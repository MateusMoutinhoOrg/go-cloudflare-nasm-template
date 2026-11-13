// Example server: Linkbox, a small app that saves links.
//
// You give it the address of a page. The server visits the page to read its
// title, and saves both in a database. It uses everything a sandbox can use:
//
//	deps.Server       reads each request and writes its response
//	deps.HTTPClient   visits the page, to read its title
//	deps.DatabaseHub  keeps the links in a SQLite database
//
// The routes:
//
//	GET    /               the page (index.html)
//	GET    /api/links      lists the links
//	POST   /api/links      saves a link, sent as {"url": "https://go.dev"}
//	DELETE /api/links/3    removes a link
//
// Replace it with your own server. Everything a sandbox can use is declared
// in /deps; how it is provided is the job of /engine.
package sandbox

import (
	_ "embed" // makes go:embed (below) work
	"html"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/MateusMoutinhoOrg/go-cloudflare-nasm-template/deps"
)

// The frontend. The file is copied into the program when it is compiled.
//
//go:embed index.html
var page []byte

// SandboxMain runs once for every HTTP request.
func SandboxMain(deps deps.Deps) {
	server := deps.Server

	switch {
	case server.Match("/"):
		server.Bytes(200, "text/html; charset=utf-8", page)
	case server.Match("/api/links"), server.Match("/api/links/:id"):
		links(server, deps.DatabaseHub, deps.HTTPClient)
	default:
		server.Text(404, "Route not found.")
	}
}

// createLinks runs once, when the database is created.
const createLinks = `
CREATE TABLE links (
  id    INTEGER PRIMARY KEY,
  url   TEXT NOT NULL,
  title TEXT NOT NULL
);
`

// links answers /api/links. It opens the database, then does what the method
// of the request asks for.
func links(server deps.Server, databases deps.DatabaseHub, client deps.HTTPClient) {
	db, err := databases.Connect("links", createLinks)
	if err != nil {
		server.Text(500, "Could not open the database: "+err.Error())
		return
	}

	switch server.Method() {
	case "GET":
		listLinks(server, db)
	case "POST":
		addLink(server, client, db)
	case "DELETE":
		removeLink(server, db)
	default:
		server.Text(405, "Use GET, POST or DELETE.")
	}
}

// listLinks responds with every saved link, the newest first.
// addLink and removeLink end with it, so the page always gets the list as it
// is now.
func listLinks(server deps.Server, db *deps.Database) {
	rows, err := db.All(`SELECT id, url, title FROM links ORDER BY id DESC`)
	if err != nil {
		server.Text(500, "Could not read the links: "+err.Error())
		return
	}
	server.JSON(200, rows) // [{"id":1,"title":"The Go Programming Language","url":"https://go.dev"}, ...]
}

// addLink saves the link sent in the body of the request. It uses
// deps.HTTPClient to visit the page and read its title.
func addLink(server deps.Server, client deps.HTTPClient, db *deps.Database) {
	var input struct {
		URL string `json:"url"`
	}
	if err := server.BindJSON(&input, 4096); err != nil {
		server.Text(400, `Send the link as JSON: {"url": "https://go.dev"}`)
		return
	}

	address := strings.TrimSpace(input.URL)
	if !strings.Contains(address, "://") {
		address = "https://" + address // "go.dev" becomes "https://go.dev"
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		server.Text(400, `"`+input.URL+`" is not the address of a page.`)
		return
	}

	// Some sites refuse a visitor that does not say who it is.
	headers := map[string]string{"User-Agent": "Mozilla/5.0 (compatible; Linkbox/1.0)"}
	response, err := client.Do("GET", address, headers, nil)
	if err != nil { // no response at all; a 404 or a 500 is not an error
		server.Text(502, "Could not reach "+address+".")
		return
	}

	// The title is at the top of the page: there is no need to read all of it.
	top, _ := io.ReadAll(io.LimitReader(response.Body(), 200_000))
	title := pageTitle(string(top))
	if title == "" {
		title = parsed.Host
	}

	if _, err := db.Run(`INSERT INTO links (url, title) VALUES (?1, ?2)`, address, title); err != nil {
		server.Text(500, "Could not save the link: "+err.Error())
		return
	}
	listLinks(server, db)
}

// removeLink deletes the link given in the route: /api/links/3
func removeLink(server deps.Server, db *deps.Database) {
	id, err := strconv.Atoi(server.Param("id")) // "" when the route is only /api/links
	if err != nil {
		server.Text(400, "Say which link: DELETE /api/links/3")
		return
	}

	if _, err := db.Run(`DELETE FROM links WHERE id = ?1`, id); err != nil {
		server.Text(500, "Could not remove the link: "+err.Error())
		return
	}
	listLinks(server, db)
}

// titleTag finds <title>...</title>, in upper or lower case, even when the
// title has line breaks.
var titleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// pageTitle returns the title of an HTML page, or "" if it has none.
func pageTitle(content string) string {
	found := titleTag.FindStringSubmatch(content)
	if found == nil {
		return ""
	}
	title := html.UnescapeString(found[1])          // "Tom &amp; Jerry" becomes "Tom & Jerry"
	return strings.Join(strings.Fields(title), " ") // no line breaks, no repeated spaces
}
