package collection

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mesinkasir/gax/internal/parser"
)

type Post struct {
	Title string
	URL string
	Date time.Time
	DateStr string
	Tags []string
	Layout string
	BodyHTML string
	Frontmatter map[string]interface{}
	Prev *Post
	Next *Post
	Data map[string]interface{}
	Collection string
}

func Build(contentDir string, globalData map[string]interface{}) (map[string][]*Post, []*Post, map[string][]*Post) {
	collections := make(map[string][]*Post)
	tagsMap := make(map[string][]*Post)
	var all []*Post

	filepath.WalkDir(contentDir, func(path string, d os.DirEntry, err error) error {
		if d.IsDir() || filepath.Ext(path)!= ".md" {
			return nil
		}

		rel, _ := filepath.Rel(contentDir, path)
		if!strings.Contains(rel, string(os.PathSeparator)) {
			return nil
		}

		b, _ := os.ReadFile(path)
		fm, body := parser.ParseFile(string(b))

		title, _ := fm["title"].(string)
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), ".md")
		}

		dateStr, _ := fm["date"].(string)
		if dateStr == "" {
			dateStr, _ = fm["Date"].(string)
		}

		var date time.Time
		if dateStr!= "" {
			date, _ = time.Parse("2006-01-02", dateStr)
		}
		if date.IsZero() {
			info, _ := d.Info()
			date = info.ModTime()
			dateStr = date.Format("2006-01-02")
		}

		layout, _ := fm["layout"].(string)

		tags := parseTags(fm)

		relDir := filepath.Dir(rel)
		slug := strings.TrimSuffix(filepath.Base(rel), ".md")
		url := buildURL(relDir, slug, fm)

		html := parser.MdToHTML(body)

		parts := strings.Split(filepath.ToSlash(rel), "/")
		collectionName := parts[0]

		p := &Post{
			Title: title,
			URL: url,
			Date: date,
			DateStr: dateStr,
			Tags: tags,
			Layout: layout,
			BodyHTML: html,
			Frontmatter: fm,
			Collection: collectionName,
		}

		m := make(map[string]interface{})
		for k, v := range fm {
			m[k] = v
		}
		m["title"] = title
		m["url"] = url
		m["content"] = html
		m["date"] = date.Format("02 Jan 2006")
		m["date_iso"] = date.Format("2006-01-02")
		m["date_raw"] = dateStr
		m["tags"] = tags
		m["collection"] = collectionName
		p.Data = m

		collections[collectionName] = append(collections[collectionName], p)
		all = append(all, p)

		for _, tg := range tags {
			tg = strings.ToLower(strings.TrimSpace(tg))
			if tg!= "" {
				tagsMap[tg] = append(tagsMap[tg], p)
			}
		}

		return nil
	})

	for k := range collections {
		sort.Slice(collections[k], func(i, j int) bool {
			return collections[k][i].Date.After(collections[k][j].Date)
		})

		for i, post := range collections[k] {
			if i > 0 {
				post.Prev = collections[k][i-1]
				post.Data["prev_post"] = map[string]interface{}{
					"title": collections[k][i-1].Title,
					"url": collections[k][i-1].URL,
					"date": collections[k][i-1].DateStr,
				}
			}
			if i < len(collections[k])-1 {
				post.Next = collections[k][i+1]
				post.Data["next_post"] = map[string]interface{}{
					"title": collections[k][i+1].Title,
					"url": collections[k][i+1].URL,
					"date": collections[k][i+1].DateStr,
				}
			}
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Date.After(all[j].Date)
	})

	return collections, all, tagsMap
}

func parseTags(fm map[string]interface{}) []string {
	var tags []string
	t, ok := fm["tags"]
	if!ok { return tags }
	switch v := t.(type) {
	case []interface{}:
		for _, x := range v {
			if s, ok := x.(string); ok {
				if s!= "" { tags = append(tags, strings.TrimSpace(s)) }
			}
		}
	case []string:
		tags = v
	case string:
		s := strings.Trim(v, "[]\"' ")
		if strings.Contains(s, ",") {
			for _, p := range strings.Split(s, ",") {
				p = strings.TrimSpace(p)
				if p!= "" { tags = append(tags, p) }
			}
		} else if s!= "" {
			tags = append(tags, strings.TrimSpace(s))
		}
	}
	return tags
}

func buildURL(relDir, slug string, fm map[string]interface{}) string {
	permalink, _ := fm["permalink"].(string)
	if permalink!= "" {
		if!strings.HasPrefix(permalink, "/") {
			permalink = "/" + permalink
		}
		if!strings.HasSuffix(permalink, "/") {
			permalink = permalink + "/"
		}
		return permalink
	}
	return "/" + filepath.ToSlash(filepath.Join(relDir, slug)) + "/"
}