package fetcher

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/hi20160616/exhtml"
	"github.com/hi20160616/gears"
	"github.com/hi20160616/ms-ltn/configs"
	"github.com/hycka/gocc"
	"github.com/pkg/errors"
	"golang.org/x/net/html"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Article struct {
	Id            string
	Title         string
	Content       string
	WebsiteId     string
	WebsiteDomain string
	WebsiteTitle  string
	UpdateTime    *timestamppb.Timestamp
	U             *url.URL
	raw           []byte
	doc           *html.Node
}

func NewArticle() *Article {
	return &Article{
		WebsiteDomain: configs.Data.MS["ltn"].Domain,
		WebsiteTitle:  configs.Data.MS["ltn"].Title,
		WebsiteId:     fmt.Sprintf("%x", md5.Sum([]byte(configs.Data.MS["ltn"].Domain))),
	}
}

// List get all articles from database
func (a *Article) List() ([]*Article, error) {
	return load()
}

// Get read database and return the data by rawurl.
func (a *Article) Get(id string) (*Article, error) {
	as, err := load()
	if err != nil {
		return nil, err
	}

	for _, a := range as {
		if a.Id == id {
			return a, nil
		}
	}
	return nil, fmt.Errorf("[%s] no article with id: %s",
		configs.Data.MS["ltn"].Title, id)
}

func (a *Article) Search(keyword ...string) ([]*Article, error) {
	as, err := load()
	if err != nil {
		return nil, err
	}

	as2 := []*Article{}
	for _, a := range as {
		for _, v := range keyword {
			v = strings.ToLower(strings.TrimSpace(v))
			switch {
			case a.Id == v:
				as2 = append(as2, a)
			case a.WebsiteId == v:
				as2 = append(as2, a)
			case strings.Contains(strings.ToLower(a.Title), v):
				as2 = append(as2, a)
			case strings.Contains(strings.ToLower(a.Content), v):
				as2 = append(as2, a)
			case strings.Contains(strings.ToLower(a.WebsiteDomain), v):
				as2 = append(as2, a)
			case strings.Contains(strings.ToLower(a.WebsiteTitle), v):
				as2 = append(as2, a)
			}
		}
	}
	return as2, nil
}

type ByUpdateTime []*Article

func (u ByUpdateTime) Len() int      { return len(u) }
func (u ByUpdateTime) Swap(i, j int) { u[i], u[j] = u[j], u[i] }
func (u ByUpdateTime) Less(i, j int) bool {
	return u[i].UpdateTime.AsTime().Before(u[j].UpdateTime.AsTime())
}

var timeout = func() time.Duration {
	t, err := time.ParseDuration(configs.Data.MS["ltn"].Timeout)
	if err != nil {
		log.Printf("[%s] timeout init error: %v", configs.Data.MS["ltn"].Title, err)
		return time.Duration(1 * time.Minute)
	}
	return t
}()

// fetchArticle fetch article by rawurl
func (a *Article) fetchArticle(rawurl string) (*Article, error) {
	translate := func(x string, err error) (string, error) {
		if err != nil {
			return "", err
		}
		tw2s, err := gocc.New("tw2s")
		if err != nil {
			return "", err
		}
		return tw2s.Convert(x)
	}

	var err error
	a.U, err = url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	// Dail
	a.raw, a.doc, err = exhtml.GetRawAndDoc(a.U, timeout)
	if err != nil {
		return nil, err
	}

	a.Id = fmt.Sprintf("%x", md5.Sum([]byte(rawurl)))

	a.Title, err = translate(a.fetchTitle())
	if err != nil {
		return nil, err
	}

	a.UpdateTime, err = a.fetchUpdateTime()
	if err != nil {
		return nil, err
	}

	// content should be the last step to fetch
	a.Content, err = translate(a.fetchContent())
	if err != nil {
		return nil, err
	}

	a.Content, err = a.fmtContent(a.Content)
	if err != nil {
		return nil, err
	}
	return a, nil

}

func (a *Article) fetchTitle() (string, error) {
	if a.doc == nil {
		return "", fmt.Errorf("[%s] a.doc is nil", configs.Data.MS["ltn"].Title)
	}
	n := exhtml.ElementsByTag(a.doc, "title")
	if n == nil {
		return "", fmt.Errorf("[%s] getTitle error, there is no element <title>", configs.Data.MS["ltn"].Title)
	}
	title := n[0].FirstChild.Data
	if strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") ||
		strings.Contains(title, "- ??????") {
		return "", fmt.Errorf("[%s] ignore post on purpose: %s",
			configs.Data.MS["ltn"].Title, a.U.String())
	}
	rp := strings.NewReplacer(" - ?????????????????????", "")
	title = strings.TrimSpace(rp.Replace(title))
	gears.ReplaceIllegalChar(&title)
	return title, nil
}

func (a *Article) fetchUpdateTime() (*timestamppb.Timestamp, error) {
	if a.doc == nil {
		return nil, errors.Errorf("[%s] fetchUpdateTime: doc is nil: %s", configs.Data.MS["ltn"].Title, a.U.String())
	}
	metas := exhtml.MetasByProperty(a.doc, "article:published_time")
	cs := []string{}
	for _, meta := range metas {
		for _, a := range meta.Attr {
			if a.Key == "content" {
				cs = append(cs, a.Val)
			}
		}
	}
	if len(cs) <= 0 {
		return nil, fmt.Errorf("[%s] fetchUpdateTime got nothing.",
			configs.Data.MS["ltn"].Title)
	}
	t, err := time.Parse(time.RFC3339, cs[0])
	if err != nil {
		return nil, err
	}
	return timestamppb.New(t), nil
}

func shanghai(t time.Time) time.Time {
	loc := time.FixedZone("UTC", 8*60*60)
	return t.In(loc)
}

func (a *Article) fetchContent() (string, error) {
	if a.raw == nil {
		return "", errors.Errorf("[%s] fetchContent: raw is nil: %s", configs.Data.MS["ltn"].Title, a.U.String())
	}
	raw := a.raw
	// Fetch content nodes
	r := exhtml.DivWithAttr2(raw, "data-desc", "?????????")
	ps := [][]byte{}
	b := bytes.Buffer{}
	re := regexp.MustCompile(`<p>(.*?)</p>`)
	for _, v := range re.FindAllSubmatch(r, -1) {
		ps = append(ps, v[1])
	}
	if len(ps) == 0 {
		return "", fmt.Errorf("no <p> matched")
	}
	for _, p := range ps {
		b.Write(p)
		b.Write([]byte("  \n"))
	}
	body := b.String()
	re = regexp.MustCompile(`???`)
	body = re.ReplaceAllString(body, "???")
	re = regexp.MustCompile(`???`)
	body = re.ReplaceAllString(body, "???")
	re = regexp.MustCompile(`<a.*?>`)
	body = re.ReplaceAllString(body, "")
	re = regexp.MustCompile(`</a>`)
	body = re.ReplaceAllString(body, "")
	re = regexp.MustCompile(`<script.*?</script>`)
	body = re.ReplaceAllString(body, "")
	re = regexp.MustCompile(`<blockquote.*?</blockquote>`)
	body = re.ReplaceAllString(body, "")
	re = regexp.MustCompile(`<iframe.*?</iframe>`)
	body = re.ReplaceAllString(body, "")

	return body, nil
}

func (a *Article) fmtContent(body string) (string, error) {
	var err error
	title := "# " + a.Title + "\n\n"
	lastupdate := shanghai(a.UpdateTime.AsTime()).Format(time.RFC3339)
	webTitle := fmt.Sprintf(" @ [%s](/list/?v=%[1]s): [%[2]s](http://%[2]s)", a.WebsiteTitle, a.WebsiteDomain)
	u, err := url.QueryUnescape(a.U.String())
	if err != nil {
		u = a.U.String() + "\n\nunescape url error:\n" + err.Error()
	}

	body = title +
		"LastUpdate: " + lastupdate +
		webTitle + "\n\n" +
		"---\n" +
		body + "\n\n" +
		"????????????" + fmt.Sprintf("[%s](%[1]s)", u)
	return body, nil
}
