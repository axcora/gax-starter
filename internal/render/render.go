package render

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mesinkasir/gax/internal/collection"
	"github.com/mesinkasir/gax/internal/parser"
)

type Ctx map[string]interface{}

func BuildSite(cols map[string][]*collection.Post, allPosts []*collection.Post, tagsMap map[string][]*collection.Post, global map[string]interface{}, tmplDir, contentDir, outDir, publicDir string, live bool) {
	os.RemoveAll(outDir)
	os.MkdirAll(outDir, 0755)
	copyPublic(publicDir, outDir)
	templates := loadTemplates(tmplDir)
	buildContentPages(contentDir, outDir, templates, cols, allPosts, global, tagsMap, live)
	buildCollectionLists(contentDir, outDir, templates, cols, global, tagsMap, live)
	buildPostsInfo(outDir, templates, cols, global, tagsMap, live)
	buildTagPages(outDir, templates, tagsMap, global, cols, live)
	buildSitemap(outDir, cols, allPosts, global)
	buildRSS(outDir, allPosts, global)
}

func copyPublic(publicDir, outDir string) {
	filepath.WalkDir(publicDir, func(p string, d fs.DirEntry, err error) error {
		if d.IsDir() { return nil }
		rel, _ := filepath.Rel(publicDir, p)
		dst := filepath.Join(outDir, rel)
		os.MkdirAll(filepath.Dir(dst), 0755)
		b, _ := os.ReadFile(p)
		os.WriteFile(dst, b, 0644)
		return nil
	})
}

func loadTemplates(tmplDir string) map[string]string {
	templates := map[string]string{}
	filepath.WalkDir(tmplDir, func(p string, d fs.DirEntry, err error) error {
		if!d.IsDir() && filepath.Ext(p) == ".gax" {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(tmplDir, p)
			templates[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	return templates
}

func buildContext(global map[string]interface{}, fm map[string]interface{}, live bool) Ctx {
	ctx := make(Ctx)
	for k, v := range global { ctx[k] = v }
	for k, v := range fm { ctx[k] = v }
	if cfg, ok := global["config"].(map[string]interface{}); ok {
		for k, v := range cfg { if _, exists := ctx[k];!exists { ctx[k] = v } }
	}
	if live { ctx["__live"] = true }
	return ctx
}

func buildAllTags(tagsMap map[string][]*collection.Post) []map[string]interface{} {
	var allTags []map[string]interface{}
	for tag, posts := range tagsMap {
		allTags = append(allTags, map[string]interface{}{"name": tag, "slug": tag, "count": len(posts), "url": "/tags/" + tag + "/"})
	}
	sort.Slice(allTags, func(i, j int) bool { return allTags[i]["name"].(string) < allTags[j]["name"].(string) })
	return allTags
}

func getPagination(posts []*collection.Post, page, perPage int, collName string) map[string]interface{} {
	totalPages := (len(posts) + perPage - 1) / perPage
	if totalPages == 0 { totalPages = 1 }
	start := (page - 1) * perPage
	end := start + perPage
	if end > len(posts) { end = len(posts) }
	items := posts[start:end]
	var tmplItems []map[string]interface{}
	for _, p := range items { tmplItems = append(tmplItems, p.Data) }
	prevURL := ""; if page > 1 { if page == 2 { prevURL = "/" + collName + "/" } else { prevURL = "/" + collName + "/page/" + strconv.Itoa(page-1) + "/" } }
	nextURL := ""; if page < totalPages { nextURL = "/" + collName + "/page/" + strconv.Itoa(page+1) + "/" }
	return map[string]interface{}{"items": tmplItems, "current_page": page, "total_pages": totalPages, "prev_url": prevURL, "next_url": nextURL}
}

func buildContentPages(contentDir, outDir string, tmpl map[string]string, cols map[string][]*collection.Post, allPosts []*collection.Post, global map[string]interface{}, tagsMap map[string][]*collection.Post, live bool) {
	filepath.WalkDir(contentDir, func(path string, d fs.DirEntry, err error) error {
		if err!= nil || d.IsDir() || filepath.Ext(path)!= ".md" { return nil }
		rel, _ := filepath.Rel(contentDir, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) == 1 {
			b, _ := os.ReadFile(path)
			fm, _ := parser.ParseFile(string(b))
			if _, ok := fm["collection"].(string); ok { return nil }
		}
		b, _ := os.ReadFile(path)
		fm, rawBody := parser.ParseFile(string(b))

		layoutName, _ := fm["layout"].(string)
		layout := findLayout(layoutName, tmpl)
		if layout == "" { layout = "layouts/base.gax" }

		ctx := buildContext(global, fm, live)
		ctx["collections"] = cols
		ctx["all_tags"] = buildAllTags(tagsMap)

		if tocVal, ok := fm["toc"]; ok {
			shouldTOC := false
			switch v := tocVal.(type) {
			case bool:
				shouldTOC = v
			case string:
				shouldTOC = (v == "true" || v == "True" || v == "TRUE")
			}
			if shouldTOC {
				tocHTML := parser.GenerateTOC(rawBody)
				ctx["toc"] = tocHTML
				ctx["has_toc"] = true
			}
		}

		body := renderString(rawBody, tmpl, ctx)
		html := parser.MdToHTML(body)

		ctx["content"] = html

		siteURL := getSiteURL(global)
nameTmp := strings.TrimSuffix(d.Name(), ".md")
relDirTmp := filepath.Dir(rel)
var urlPath string
if nameTmp == "index" {
	if relDirTmp == "." { urlPath = "/" } else { urlPath = "/" + filepath.ToSlash(relDirTmp) + "/" }
} else {
	if relDirTmp == "." { urlPath = "/" + nameTmp + "/" } else { urlPath = "/" + filepath.ToSlash(relDirTmp) + "/" + nameTmp + "/" }
}
ctx["canonical"] = siteURL + urlPath
ctx["permalink"] = siteURL + urlPath

		if len(parts) > 0 {
			collectionName := parts[0]
			if posts, ok := cols[collectionName]; ok {
				title, _ := fm["title"].(string)
				if title == "" { title = strings.TrimSuffix(d.Name(), ".md") }
				for i, p := range posts {
					if p.Title == title {
						if i > 0 { ctx["prev_post"] = map[string]interface{}{"title": posts[i-1].Title, "url": posts[i-1].URL, "date": posts[i-1].DateStr} }
						if i < len(posts)-1 { ctx["next_post"] = map[string]interface{}{"title": posts[i+1].Title, "url": posts[i+1].URL, "date": posts[i+1].DateStr} }
						break
					}
				}
			}
		}

		outHTML := renderGax(tmpl, layout, ctx)
		name := strings.TrimSuffix(d.Name(), ".md")
		relDir := filepath.Dir(rel)
		var outPath string
		if name == "index" { outPath = filepath.Join(outDir, relDir, "index.html") } else { outPath = filepath.Join(outDir, relDir, name, "index.html") }
		os.MkdirAll(filepath.Dir(outPath), 0755)
		os.WriteFile(outPath, []byte(outHTML), 0644)
		return nil
	})
}

func buildCollectionLists(contentDir, outDir string, tmpl map[string]string, cols map[string][]*collection.Post, global map[string]interface{}, tagsMap map[string][]*collection.Post, live bool) {
	entries, _ := os.ReadDir(contentDir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name())!= ".md" { continue }
		path := filepath.Join(contentDir, e.Name())
		b, _ := os.ReadFile(path)
		fm, _ := parser.ParseFile(string(b))
		collName, _ := fm["collection"].(string)
		if collName == "" { continue }
		layoutName, _ := fm["layout"].(string)
		layout := findLayout(layoutName, tmpl)
		if layout == "" { continue }
		perPage := 10
		if p, ok := fm["pagination"]; ok {
			switch v := p.(type) {
			case int: perPage = v
			case string: if i, err := strconv.Atoi(v); err == nil { perPage = i }
			case float64: perPage = int(v)
			}
		}
		posts := cols[collName]
		if posts == nil { continue }
		totalPages := (len(posts) + perPage - 1) / perPage
		if totalPages == 0 { totalPages = 1 }
		for page := 1; page <= totalPages; page++ {
			pagination := getPagination(posts, page, perPage, collName)
			ctx := buildContext(global, fm, live)
			siteURL := getSiteURL(global)
	canonical := siteURL + "/" + collName + "/"
	if page > 1 {
		canonical = siteURL + "/" + collName + "/page/" + strconv.Itoa(page) + "/"
	}
	ctx["canonical"] = canonical
	ctx["permalink"] = canonical
			ctx["collections"] = cols
			ctx["all_tags"] = buildAllTags(tagsMap)
			ctx["pagination"] = pagination
			outHTML := renderGax(tmpl, layout, ctx)
			var outPath string
			if page == 1 { outPath = filepath.Join(outDir, collName, "index.html") } else { outPath = filepath.Join(outDir, collName, "page", strconv.Itoa(page), "index.html") }
			os.MkdirAll(filepath.Dir(outPath), 0755)
			os.WriteFile(outPath, []byte(outHTML), 0644)
		}
	}
}

func buildPostsInfo(outDir string, tmpl map[string]string, cols map[string][]*collection.Post, global map[string]interface{}, tagsMap map[string][]*collection.Post, live bool) {
	var defaultCollName string
	var defaultPosts []*collection.Post
	for name, posts := range cols { if len(posts) > 0 { defaultCollName = name; defaultPosts = posts; break } }
	if defaultPosts == nil { return }
	layout := "layouts/collection.gax"
	if _, ok := tmpl[layout];!ok { layout = "layouts/base.gax" }
	perPage := 6
	allTags := buildAllTags(tagsMap)
	totalPages := (len(defaultPosts) + perPage - 1) / perPage
	if totalPages == 0 { totalPages = 1 }
	for page := 1; page <= totalPages; page++ {
		pagination := getPagination(defaultPosts, page, perPage, defaultCollName)
		ctx := buildContext(global, map[string]interface{}{}, live)
		ctx["collections"] = cols
		ctx["all_tags"] = allTags
		ctx["collection_name"] = defaultCollName
		ctx["collection_posts"] = defaultPosts
		ctx["pagination"] = pagination
		outHTML := renderGax(tmpl, layout, ctx)
		var outPath string
		if page == 1 { outPath = filepath.Join(outDir, "posts", "info", "index.html") } else { outPath = filepath.Join(outDir, "posts", "info", "page", strconv.Itoa(page), "index.html") }
		os.MkdirAll(filepath.Dir(outPath), 0755)
		os.WriteFile(outPath, []byte(outHTML), 0644)
	}
}

func buildTagPages(outDir string, tmpl map[string]string, tagsMap map[string][]*collection.Post, global map[string]interface{}, cols map[string][]*collection.Post, live bool) {
	allTags := buildAllTags(tagsMap)
	tagListFM := map[string]interface{}{}
	tagListPath := filepath.Join("content", "tags.md")
	if b, err := os.ReadFile(tagListPath); err == nil {
		fm, _ := parser.ParseFile(string(b))
		tagListFM = fm
	}
	if _, ok := tmpl["layouts/tags-list.gax"]; ok {
		ctx := buildContext(global, tagListFM, live)
		ctx["all_tags"] = allTags
		ctx["collections"] = cols
		ctx["canonical"] = getSiteURL(global) + "/tags/"
		ctx["permalink"] = getSiteURL(global) + "/tags/"
		outHTML := renderGax(tmpl, "layouts/tags-list.gax", ctx)
		os.MkdirAll(filepath.Join(outDir, "tags"), 0755)
		os.WriteFile(filepath.Join(outDir, "tags", "index.html"), []byte(outHTML), 0644)
	}
	for tag, posts := range tagsMap {
		var items []map[string]interface{}
		for _, p := range posts { items = append(items, p.Data) }
		ctx := buildContext(global, map[string]interface{}{}, live)
		ctx["tag"] = tag
		ctx["title"] = strings.Title(tag)
		ctx["posts"] = items
		ctx["collections"] = cols
		ctx["all_tags"] = allTags
		ctx["canonical"] = getSiteURL(global) + "/tags/" + tag + "/"
		ctx["permalink"] = getSiteURL(global) + "/tags/" + tag + "/"
		layout := "layouts/tag.gax"
		if _, ok := tmpl[layout];!ok { layout = "layouts/base.gax" }
		outHTML := renderGax(tmpl, layout, ctx)
		outPath := filepath.Join(outDir, "tags", tag, "index.html")
		os.MkdirAll(filepath.Dir(outPath), 0755)
		os.WriteFile(outPath, []byte(outHTML), 0644)
	}
}

func buildSitemap(outDir string, cols map[string][]*collection.Post, allPosts []*collection.Post, global map[string]interface{}) {
	siteURL := getSiteURL(global)
	var urls []string
	urls = append(urls, siteURL+"/")
	for _, posts := range cols { for _, p := range posts { urls = append(urls, siteURL+p.URL) } }
	urls = append(urls, siteURL+"/tags/", siteURL+"/posts/info/")
	seen := map[string]bool{}; var uniq []string
	for _, u := range urls { if!seen[u] { seen[u] = true; uniq = append(uniq, u) } }
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, u := range uniq { sb.WriteString(fmt.Sprintf("<url><loc>%s</loc><lastmod>%s</lastmod></url>", u, time.Now().Format("2006-01-02"))) }
	sb.WriteString("</urlset>")
	os.WriteFile(filepath.Join(outDir, "sitemap.xml"), []byte(sb.String()), 0644)
	os.WriteFile(filepath.Join(outDir, "robots.txt"), []byte(fmt.Sprintf("User-agent: *\nAllow: /\nSitemap: %s/sitemap.xml\n", siteURL)), 0644)
}

func buildRSS(outDir string, allPosts []*collection.Post, global map[string]interface{}) {
	siteTitle, siteDesc, siteURL := getSiteInfo(global)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>%s</title><link>%s</link><description>%s</description>`, siteTitle, siteURL, siteDesc))
	for i, p := range allPosts {
		if i >= 20 { break }
		sb.WriteString(fmt.Sprintf("<item><title><![CDATA[%s]]></title><link>%s%s</link><guid>%s%s</guid><pubDate>%s</pubDate><description><![CDATA[%s]]></description></item>", p.Title, siteURL, p.URL, siteURL, p.URL, p.Date.Format(time.RFC1123Z), p.BodyHTML))
	}
	sb.WriteString("</channel></rss>")
	rssContent := sb.String()
	os.WriteFile(filepath.Join(outDir, "rss.xml"), []byte(rssContent), 0644)
	os.WriteFile(filepath.Join(outDir, "feed.xml"), []byte(rssContent), 0644)
	os.WriteFile(filepath.Join(outDir, "atom.xml"), []byte(rssContent), 0644)
}

func getSiteURL(global map[string]interface{}) string {
	siteURL := "https://example.com"
	if md, ok := global["metadata"].(map[string]interface{}); ok {
		if u, ok := md["url"].(string); ok && u!= "" { siteURL = u }
		if u, ok := md["site_url"].(string); ok && u!= "" { siteURL = u }
	}
	if cfg, ok := global["config"].(map[string]interface{}); ok {
		if u, ok := cfg["url"].(string); ok && u!= "" { siteURL = u }
	}
	return strings.TrimSuffix(siteURL, "/")
}

func findLayout(name string, tmpl map[string]string) string {
	if name == "" { return "" }
	if!strings.HasSuffix(name, ".gax") { name = name + ".gax" }
	paths := []string{name, "layouts/" + name}
	for _, p := range paths { if _, ok := tmpl[p]; ok { return p } }
	return ""
}

func getSiteInfo(global map[string]interface{}) (string, string, string) {
	siteTitle := "GAX Site"; siteDesc := "Built with GAX"; siteURL := "https://example.com"
	if md, ok := global["metadata"].(map[string]interface{}); ok {
		if t, ok := md["title"].(string); ok { siteTitle = t }
		if d, ok := md["description"].(string); ok { siteDesc = d }
		if u, ok := md["url"].(string); ok { siteURL = u }
	}
	return siteTitle, siteDesc, siteURL
}

func renderGax(tmpl map[string]string, layout string, ctx Ctx) string {
	content := ""
	for {
		src, ok := tmpl[layout]
		if!ok {
			found := findLayout(layout, tmpl)
			if found!= "" { src = tmpl[found]; ok = true; layout = found }
			if!ok { return content + "<!-- missing " + layout + " -->" }
		}
		fm, body := parser.ParseFile(src)
		if content!= "" { ctx["content"] = content }
		rendered := renderString(body, tmpl, ctx)
		content = rendered
		if parent, ok := fm["layout"].(string); ok && parent!= "" {
			layout = findLayout(parent, tmpl)
			if layout == "" { break }
			continue
		}
		break
	}
	if ctx["__live"] == true {
		liveScript := `<script>let _h="";setInterval(()=>fetch('/__ping').then(r=>r.text()).then(t=>{if(_h&&_h!=t)location.reload();_h=t}),1000)</script>`
		if strings.Contains(content, "</body>") { content = strings.Replace(content, "</body>", liveScript+"</body>", 1) } else { content += liveScript }
	}
	return content
}

func renderString(src string, tmpl map[string]string, ctx Ctx) string {
	// 1. PROTECT CODE BLOCK ```...``` dan `...` BIAR GAK KE-RENDER
	codeBlocks := map[string]string{}
	placeholder := "___GAX_CODE_BLOCK_%d___"
	idx := 0

	reCodeBlock := regexp.MustCompile("(?s)```.*?```")
	src = reCodeBlock.ReplaceAllStringFunc(src, func(m string) string {
		key := fmt.Sprintf(placeholder, idx)
		codeBlocks[key] = m
		idx++
		return key
	})

	reInlineCode := regexp.MustCompile("`[^`\n]+`")
	src = reInlineCode.ReplaceAllStringFunc(src, func(m string) string {
		
		if strings.Contains(m, "{%") || strings.Contains(m, "{{") {
			
			key := fmt.Sprintf(placeholder, idx)
			codeBlocks[key] = m
			idx++
			return key
		}
		key := fmt.Sprintf(placeholder, idx)
		codeBlocks[key] = m
		idx++
		return key
	})

	src = processIncludes(src, tmpl, ctx)
	src = processForLoops(src, tmpl, ctx)
	src = processIfConditions(src, tmpl, ctx)
	src = processVariables(src, ctx)

	for key, original := range codeBlocks {
		src = strings.ReplaceAll(src, key, original)
	}

	return src
}

func processIncludes(src string, tmpl map[string]string, ctx Ctx) string {
	reInc := regexp.MustCompile(`{%\s*include\s+["'](.+?)["']\s*%}`)
	return reInc.ReplaceAllStringFunc(src, func(m string) string {
		sub := reInc.FindStringSubmatch(m)[1]
		paths := []string{sub, sub + ".gax", "layouts/" + sub, "layouts/" + sub + ".gax", "partials/" + sub, "partials/" + sub + ".gax"}
		for _, p := range paths {
			if t, ok := tmpl[p]; ok {
				_, body := parser.ParseFile(t)
				return renderString(body, tmpl, ctx)
			}
		}
		return "<!-- include not found: " + sub + " -->"
	})
}

func processForLoops(src string, tmpl map[string]string, ctx Ctx) string {
	reHead := regexp.MustCompile(`{%\s*for\s+(\w+)\s+in\s+(.+?)\s*%}`)
	for {
		si := strings.Index(src, "{% for")
		if si == -1 { break }
		ei := strings.Index(src[si:], "%}")
		if ei == -1 { break }
		ei += si
		header := src[si : ei+2]
		m := reHead.FindStringSubmatch(header)
		if len(m) < 3 { break }
		varName := m[1]
		expr := strings.TrimSpace(m[2])

		innerStart := ei + 2
		depth := 1
		sp := innerStart
		innerEnd := -1
		for depth > 0 {
			nf := strings.Index(src[sp:], "{% for")
			ne := strings.Index(src[sp:], "{% endfor %}")
			if ne == -1 { break }
			if nf!= -1 && nf < ne { depth++; sp += nf + 6 } else { depth--; if depth == 0 { innerEnd = sp + ne; break }; sp += ne + 12 }
		}
		if innerEnd == -1 { break }
		inner := src[innerStart:innerEnd]
		full := src[si : innerEnd+len("{% endfor %}")]

		base, tag, limit, orderBy := parseForExpr(expr)

		collVal := resolveVar(base, ctx)
		var list []interface{}
		switch v := collVal.(type) {
		case []map[string]interface{}:
			for _, x := range v { list = append(list, x) }
		case []interface{}:
			list = v
		case []*collection.Post:
			for _, x := range v { list = append(list, x.Data) }
		}

		if tag!= "" { list = filterByTag(list, tag) }

		if orderBy!= "" {
    
    desc := false
    field := orderBy
    if strings.Contains(orderBy, ":") {
        parts := strings.Split(orderBy, ":")
        field = parts[0]
        if len(parts) > 1 && (parts[1] == "desc" || parts[1] == "DESC") {
            desc = true
        }
    }
    
    if field == "date" { desc = true }

    sort.SliceStable(list, func(i, j int) bool {
        mi, ok1 := list[i].(map[string]interface{})
        mj, ok2 := list[j].(map[string]interface{})
        if!ok1 ||!ok2 { return false }
        vi := mi[field]
        vj := mj[field]

        
        if field == "date" {
            var ti, tj time.Time
            switch a := vi.(type) {
            case time.Time: ti = a
            case string: ti, _ = time.Parse("2006-01-02", a)
            }
            switch b := vj.(type) {
            case time.Time: tj = b
            case string: tj, _ = time.Parse("2006-01-02", b)
            }
            if!ti.IsZero() &&!tj.IsZero() {
                if desc { return ti.After(tj) } else { return ti.Before(tj) }
            }
        }

        
        var ii, jj int
        var isInt bool
        switch a := vi.(type) {
        case int: ii = a; isInt = true
        case float64: ii = int(a); isInt = true
        case string:
            if n, err := strconv.Atoi(a); err == nil { ii = n; isInt = true }
        }
        switch b := vj.(type) {
        case int: jj = b
        case float64: jj = int(b)
        case string:
            if n, err := strconv.Atoi(b); err == nil { jj = n }
        }
        if isInt {
            if desc { return ii > jj } else { return ii < jj }
        }
        if desc {
            return fmt.Sprintf("%v", vi) > fmt.Sprintf("%v", vj)
        }
        return fmt.Sprintf("%v", vi) < fmt.Sprintf("%v", vj)
    })
}

		if limit >= 0 && len(list) > limit { list = list[:limit] }

		var out strings.Builder
		for _, it := range list {
			nc := cloneCtx(ctx)
			nc[varName] = it
			out.WriteString(renderString(inner, tmpl, nc))
		}
		src = strings.Replace(src, full, out.String(), 1)
	}
	return src
}

func parseForExpr(expr string) (base string, tag string, limit int, orderBy string) {
	limit = -1
	orderBy = ""
	
	parts := strings.Split(expr, "|")
	left := strings.TrimSpace(parts[0])
	toks := strings.Fields(left)
	if len(toks) > 0 { base = toks[0] }
	for _, t := range toks[1:] {
		t = strings.TrimSpace(t)
		if strings.HasPrefix(t, "filter:tag=") { tag = strings.TrimPrefix(t, "filter:tag=") }
		if strings.HasPrefix(t, "by:") {
			val := strings.Trim(strings.TrimPrefix(t, "by:"), "()")
			if val == "order" || val == "title" || val == "date" || val == "orderBy" {
				orderBy = val
				if orderBy == "orderBy" { orderBy = "order" }
			} else {
				tag = val
			}
		}
		if strings.HasPrefix(t, "orderBy:") { orderBy = strings.TrimPrefix(t, "orderBy:") }
		if strings.HasPrefix(t, "sort:") { orderBy = strings.TrimPrefix(t, "sort:") }
		if strings.HasPrefix(t, "order:") { orderBy = strings.TrimPrefix(t, "order:") }
		if strings.HasPrefix(t, "limit:") { if n, err := strconv.Atoi(strings.TrimPrefix(t, "limit:")); err == nil { limit = n } }
	}
	if len(parts) > 1 {
		right := strings.Join(parts[1:], "|")
		r1 := regexp.MustCompile(`limit\s*:\s*(\d+)`)
		r4 := regexp.MustCompile(`by:\(?([^)\s|]+)\)?`)
		r5 := regexp.MustCompile(`orderBy:\s*([^\s|]+)`)
		r6 := regexp.MustCompile(`sort:\s*([^\s|]+)`)
		if mm := r1.FindStringSubmatch(right); len(mm) == 2 { n, _ := strconv.Atoi(mm[1]); limit = n }
		if mm := r4.FindStringSubmatch(right); len(mm) == 2 {
			val := strings.Trim(mm[1], "()")
			if val == "order" || val == "title" || val == "date" { orderBy = val } else if tag == "" { tag = val }
		}
		if mm := r5.FindStringSubmatch(right); len(mm) == 2 { orderBy = mm[1] }
		if mm := r6.FindStringSubmatch(right); len(mm) == 2 { orderBy = mm[1] }
	}
	
	if strings.Contains(orderBy, ":") {
		
	}
	return strings.TrimSpace(base), strings.Trim(strings.TrimSpace(tag), "()"), limit, strings.TrimSpace(orderBy)
}

func filterByTag(list []interface{}, tag string) []interface{} {
	var out []interface{}
	tag = strings.ToLower(tag)
	for _, it := range list {
		if mm, ok := it.(map[string]interface{}); ok {
			if tv, ok := mm["tags"]; ok {
				switch v := tv.(type) {
				case []interface{}:
					for _, t := range v { if strings.ToLower(fmt.Sprint(t)) == tag { out = append(out, it); break } }
				case []string:
					for _, t := range v { if strings.ToLower(t) == tag { out = append(out, it); break } }
				case string:
					if strings.Contains(strings.ToLower(v), tag) { out = append(out, it) }
				}
			}
		}
	}
	return out
}

func processIfConditions(src string, tmpl map[string]string, ctx Ctx) string {
	reIf := regexp.MustCompile(`(?s){%\s*if\s+(.+?)\s*%}(.*?){%\s*endif\s*%}`)
	return reIf.ReplaceAllStringFunc(src, func(m string) string {
		sub := reIf.FindStringSubmatch(m)
		cond := strings.TrimSpace(sub[1])
		body := sub[2]
		conds := []string{cond}
		reElif := regexp.MustCompile(`{%\s*elif\s+(.+?)\s*%}`)
		matches := reElif.FindAllStringSubmatch(body, -1)
		for _, mm := range matches { conds = append(conds, strings.TrimSpace(mm[1])) }
		bodies := reElif.Split(body, -1)
		for idx, c := range conds {
			if evalCond(c, ctx) {
				b := bodies[idx]
				if strings.Contains(b, "{% else %}") {
					sp := strings.SplitN(b, "{% else %}", 2)
					return renderString(sp[0], tmpl, ctx)
				}
				return renderString(b, tmpl, ctx)
			}
		}
		lastBody := bodies[len(bodies)-1]
		if strings.Contains(lastBody, "{% else %}") {
			sp := strings.SplitN(lastBody, "{% else %}", 2)
			return renderString(sp[1], tmpl, ctx)
		}
		if strings.Contains(body, "{% else %}") && len(conds) == 1 {
			sp := strings.SplitN(body, "{% else %}", 2)
			return renderString(sp[1], tmpl, ctx)
		}
		return ""
	})
}

func processVariables(src string, ctx Ctx) string {
	reVar := regexp.MustCompile(`{{\s*([^}]+?)\s*}}`)
	return reVar.ReplaceAllStringFunc(src, func(m string) string {
		expr := reVar.FindStringSubmatch(m)[1]
		expr = strings.TrimSpace(expr)
		if strings.Contains(expr, "|") {
			parts := strings.Split(expr, "|")
			varName := strings.TrimSpace(parts[0])
			val := resolveVar(varName, ctx)
			for _, f := range parts[1:] {
				f = strings.TrimSpace(f)
				if strings.HasPrefix(f, "date") { val = formatDateFilter(val, f) } else if f == "upper" { val = strings.ToUpper(fmt.Sprintf("%v", val)) } else if f == "lower" { val = strings.ToLower(fmt.Sprintf("%v", val)) }
			}
			return safeDateString(val)
		}
		if strings.Contains(expr, " or ") {
			parts := strings.Split(expr, " or ")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				p = strings.Trim(p, "\"'")
				val := resolveVar(p, ctx)
				s := safeDateString(val)
				if s!= "" && s!= "<nil>" && s!= "map[]" && s!= "[]" && s!= "0" { return s }
			}
			return ""
		}
		val := resolveVar(expr, ctx)
		return safeDateString(val)
	})
}

func safeDateString(v interface{}) string {
	if v == nil { return "" }
	switch x := v.(type) {
	case time.Time: return x.Format("02 Jan 2006")
	case *time.Time: return x.Format("02 Jan 2006")
	case string:
		if t, err := time.Parse("2006-01-02", x); err == nil { return t.Format("02 Jan 2006") }
		return x
	case bool: if x { return "true" }; return ""
	default:
		t := fmt.Sprintf("%T", v)
		if strings.Contains(t, "Post") { return "" }
		return fmt.Sprintf("%v", v)
	}
}

func formatDateFilter(val interface{}, filter string) interface{} {
	re := regexp.MustCompile(`date\(["'](.+?)["']\)`)
	if m := re.FindStringSubmatch(filter); m!= nil {
		format := m[1]
		switch t := val.(type) {
		case time.Time: return t.Format(format)
		case string:
			formats := []string{"2006-01-02", "02-01-2006", "2006/01/02", time.RFC3339}
			for _, f := range formats { if dt, err := time.Parse(f, t); err == nil { return dt.Format(format) } }
			return t
		}
	}
	if t, ok := val.(time.Time); ok { return t.Format("2006-01-02") }
	return val
}

func resolveVar(expr string, ctx Ctx) interface{} {
	expr = strings.TrimSpace(expr)
	if expr == "" { return "" }
	if (strings.HasPrefix(expr, "\"") && strings.HasSuffix(expr, "\"")) || (strings.HasPrefix(expr, "'") && strings.HasSuffix(expr, "'")) { return strings.Trim(expr, "\"'") }
	parts := strings.Split(expr, ".")
	var cur interface{} = ctx
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" { continue }
		switch v := cur.(type) {
		case Ctx:
			if val, ok := v[p]; ok { cur = val; continue }
			for k, val := range v { if strings.EqualFold(k, p) { cur = val; goto next } }
			return ""
		case map[string]interface{}:
			if val, ok := v[p]; ok { cur = val; continue }
			for k, val := range v { if strings.EqualFold(k, p) { cur = val; goto next } }
			return ""
		case map[string][]*collection.Post:
			if val, ok := v[p]; ok { cur = val; continue }
		default:
			if mp, ok := cur.(map[string]interface{}); ok {
				if val, ok := mp[p]; ok { cur = val; continue }
				for k, val := range mp { if strings.EqualFold(k, p) { cur = val; goto next } }
			}
			return ""
		}
	next:
	}
	return cur
}

func cloneCtx(c Ctx) Ctx { nc := make(Ctx); for k, v := range c { nc[k] = v }; return nc }

func evalCond(cond string, ctx Ctx) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" { return false }
	if strings.Contains(cond, " in ") { return evalInCondition(cond, ctx) }
	if strings.Contains(cond, "==") { return evalEqualsCondition(cond, ctx) }
	val := resolveVar(cond, ctx)
	s := fmt.Sprintf("%v", val)
	return s!= "" && s!= "false" && s!= "<nil>" && s!= "0" && s!= "[]" && s!= "map[]"
}

func evalInCondition(cond string, ctx Ctx) bool {
	parts := strings.SplitN(cond, " in ", 2)
	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	leftVal := resolveVar(left, ctx)
	if (strings.HasPrefix(left, "'") && strings.HasSuffix(left, "'")) || (strings.HasPrefix(left, "\"") && strings.HasSuffix(left, "\"")) { leftVal = strings.Trim(left, "\"'") }
	rightVal := resolveVar(right, ctx)
	leftStr := fmt.Sprintf("%v", leftVal)
	switch rv := rightVal.(type) {
	case []string:
		for _, t := range rv { if strings.EqualFold(t, leftStr) { return true } }
		return false
	case []interface{}:
		for _, t := range rv { if strings.EqualFold(fmt.Sprintf("%v", t), leftStr) { return true } }
		return false
	case string:
		return strings.Contains(strings.ToLower(rv), strings.ToLower(leftStr))
	}
	return false
}

func evalEqualsCondition(cond string, ctx Ctx) bool {
	parts := strings.SplitN(cond, "==", 2)
	l := strings.TrimSpace(strings.Trim(strings.TrimSpace(parts[0]), "\"'"))
	r := strings.TrimSpace(strings.Trim(strings.TrimSpace(parts[1]), "\"'"))
	lv := fmt.Sprintf("%v", resolveVar(l, ctx))
	if lv == "" { lv = l }
	rv := fmt.Sprintf("%v", resolveVar(r, ctx))
	if rv == "" { rv = r }
	return lv == rv
}

func Serve(dir, addr string) {
	fmt.Println("GAX serve at http://localhost" + addr + " - CTRL+C to stop")
	http.HandleFunc("/__ping", func(w http.ResponseWriter, r *http.Request) {
		info, _ := os.Stat(filepath.Join(dir, "index.html"))
		h := ""
		if info!= nil { h = fmt.Sprintf("%d", info.ModTime().UnixNano()) }
		w.Write([]byte(h))
	})
	http.Handle("/", http.FileServer(http.Dir(dir)))
	http.ListenAndServe(addr, nil)
}