package main

import (
	"fmt"
	"html"
	"net/url"
	"strconv"
	"time"

	"strings"

	goquery "github.com/PuerkitoBio/goquery"
	colly "github.com/gocolly/colly/v2"
	tele "gopkg.in/telebot.v3"
)

type BookStorageItem struct {
	message tele.StoredMessage
	items   []*BookItem
	page    int
	sender  int64
	expires time.Time
	codes   []string
	codeMap map[string]int
}

type BookItem struct {
	Meta      string
	Title     string
	Publisher string
	Authors   string
	URL       string
	Image     string
}

var (
	selector        = &tele.ReplyMarkup{}
	bookBtnBack     = selector.Data("Back", "back")
	bookBtnDownload = selector.Data("Download", "dl", "0")
	bookStorage     = make(map[int64]map[int]*BookStorageItem)
	userSessions    = make(map[int64]map[int64]*BookStorageItem)
)

const (
	resultListLimit = 10
	shortCodeLength = 3
)

func getReply(item *BookItem) string {
	reply := ""
	if item.Image != "" {
		reply = reply + fmt.Sprintf("<a href=\"%s\">\u200b</a>\n", item.Image)
	}
	if item.Title != "" {
		reply = reply + fmt.Sprintf("📎 <b>%s</b>\n\n", html.EscapeString(item.Title))
	}
	if item.Authors != "" {
		reply = reply + fmt.Sprintf("• %s\n", html.EscapeString(item.Authors))
	}
	if item.Publisher != "" {
		reply = reply + fmt.Sprintf("• %s\n", html.EscapeString(item.Publisher))
	}
	if item.Meta != "" {
		reply = reply + fmt.Sprintf("• %s\n", html.EscapeString(item.Meta))
	}
	return reply
}

func setUserSession(chatID int64, senderID int64, item *BookStorageItem) {
	if _, ok := userSessions[chatID]; !ok {
		userSessions[chatID] = make(map[int64]*BookStorageItem)
	}
	userSessions[chatID][senderID] = item
}

func getUserSession(chatID int64, senderID int64) (*BookStorageItem, bool) {
	if chatSessions, ok := userSessions[chatID]; ok {
		item, exists := chatSessions[senderID]
		return item, exists
	}
	return nil, false
}

func saveBookStorageItem(msg *tele.Message, items []*BookItem, page int, sender int64, codes []string, codeMap map[string]int) *BookStorageItem {
	if msg == nil {
		return nil
	}
	item := &BookStorageItem{
		message: tele.StoredMessage{ChatID: msg.Chat.ID, MessageID: strconv.Itoa(msg.ID)},
		items:   items,
		page:    page,
		sender:  sender,
		codes:   codes,
		codeMap: codeMap,
		expires: time.Now().Local().Add(time.Hour * time.Duration(1)),
	}

	if _, ok := bookStorage[msg.Chat.ID]; !ok {
		bookStorage[msg.Chat.ID] = make(map[int]*BookStorageItem)
	}
	bookStorage[msg.Chat.ID][msg.ID] = item
	setUserSession(msg.Chat.ID, sender, item)

	return item
}

func generateShortCode(bookURL string, index int, used map[string]bool) string {
	code := strings.TrimSpace(strings.Trim(bookURL, "/"))
	if code == "" {
		code = fmt.Sprintf("book%d", index+1)
	}
	parts := strings.Split(code, "/")
	code = parts[len(parts)-1]
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, "_", "")
	if len(code) > shortCodeLength {
		code = code[:shortCodeLength]
	}
	code = strings.TrimSpace(code)
	if code == "" {
		code = fmt.Sprintf("book%d", index+1)
	}

	base := code
	counter := 1
	for used[code] {
		counter++
		code = fmt.Sprintf("%s%d", base, counter)
	}
	used[code] = true
	return code
}

func formatResultList(items []*BookItem, codes []string, limit int) string {
	if len(items) == 0 || limit <= 0 {
		return ""
	}
	if len(items) < limit {
		limit = len(items)
	}

	var builder strings.Builder
	builder.WriteString("Here are the top results:\n\n")

	for i := 0; i < limit; i++ {
		item := items[i]
		title := item.Title
		if title == "" {
			title = "Untitled"
		}
		code := ""
		if i < len(codes) {
			code = codes[i]
		}
		if code == "" {
			code = fmt.Sprintf("book%d", i+1)
		}
		builder.WriteString(fmt.Sprintf("%d. %s\n/%s\n\n", i+1, html.EscapeString(title), code))
	}

	return builder.String()
}

func buildResultList(items []*BookItem, limit int) (string, []string, map[string]int) {
	if len(items) == 0 || limit <= 0 {
		return "", nil, nil
	}
	if len(items) < limit {
		limit = len(items)
	}

	codes := make([]string, len(items))
	codeMap := make(map[string]int)
	usedCodes := make(map[string]bool)

	for i := 0; i < limit; i++ {
		item := items[i]
		code := generateShortCode(item.URL, i, usedCodes)
		codes[i] = code
		codeMap[strings.ToLower(code)] = i
	}

	reply := formatResultList(items, codes, limit)

	return reply, codes, codeMap
}

func BookPaginator(c tele.Context) error {
	if c.Message().Payload == "" {
		return nil
	}
	items, err := FindBook(c.Message().Payload)
	if err != nil || len(items) == 0 {
		return nil
	}

	reply, codes, codeMap := buildResultList(items, resultListLimit)
	if reply == "" {
		return nil
	}

	m, err := c.Bot().Send(c.Recipient(), reply, tele.ModeHTML)
	if err != nil {
		return err
	}

	saveBookStorageItem(m, items, 0, c.Message().Sender.ID, codes, codeMap)

	return c.Respond()
}

func renderBookDetail(c tele.Context, storageItem *BookStorageItem, index int, sender int64, editExisting bool) error {
	items := storageItem.items
	if len(items) == 0 || index < 0 || index >= len(items) {
		return c.Respond()
	}

	item := items[index]
	reply := getReply(item)

	fullURL := item.URL
	if !strings.HasPrefix(fullURL, "http") {
		fullURL = strings.TrimPrefix(fullURL, "/")
		fullURL = "https://annas-archive.org/" + fullURL
	}

	openBtn := selector.URL("Open on Anna's Archive", fullURL)
	bookBtnDownload = selector.Data("Download links", "dl", strconv.Itoa(index+1))
	selector.Inline(
		selector.Row(openBtn),
		selector.Row(bookBtnDownload),
	)

	var (
		m   *tele.Message
		err error
	)

	if editExisting {
		m, err = c.Bot().Edit(storageItem.message, reply, selector, tele.ModeHTML)
	} else {
		m, err = c.Bot().Send(c.Chat(), reply, selector, tele.ModeHTML)
	}
	if err != nil {
		return c.Respond()
	}
	saveBookStorageItem(m, items, index+1, sender, storageItem.codes, storageItem.codeMap)

	return c.Respond()
}

func BackPage(c tele.Context) error {
	mc := c.Callback().Message

	chatStorage, ok := bookStorage[mc.Chat.ID]
	if !ok {
		return c.Respond()
	}
	bi, ok := chatStorage[mc.ID]
	if !ok {
		return c.Respond()
	}
	bookItem := bi
	if bookItem.sender != c.Callback().Sender.ID {
		fmt.Println("ID don't match: ", bookItem.sender, c.Callback().Sender.ID)
		return c.Respond(&tele.CallbackResponse{
			Text: "This is not for you, you silly goober",
		})
	}

	index := bookItem.page - 1
	if index < 0 {
		return c.Respond()
	}

	return renderBookDetail(c, bookItem, index, c.Callback().Sender.ID, true)
}

func HandleShortCodeCommand(c tele.Context) error {
	if c.Message() == nil {
		return nil
	}

	text := strings.TrimSpace(c.Text())
	if text == "" || !strings.HasPrefix(text, "/") {
		return nil
	}

	firstWord := strings.Fields(text)[0]
	if firstWord == "" {
		return nil
	}
	if strings.HasPrefix(firstWord, "/books") {
		return nil
	}

	command := strings.TrimPrefix(firstWord, "/")
	if atIdx := strings.Index(command, "@"); atIdx > -1 {
		command = command[:atIdx]
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	chat := c.Chat()
	sender := c.Sender()
	if chat == nil || sender == nil {
		return nil
	}

	session, ok := getUserSession(chat.ID, sender.ID)
	if !ok || session == nil {
		return nil
	}
	if session.expires.Before(time.Now()) {
		return nil
	}

	index, found := session.codeMap[strings.ToLower(command)]
	if !found {
		return nil
	}

	return renderBookDetail(c, session, index, sender.ID, false)
}

func DownloadItem(c tele.Context) error {
	cd := c.Callback().Data
	mc := c.Callback().Message
	if cd == "" {
		c.Respond()
	}
	conv, err := strconv.Atoi(cd)
	if err != nil {
		c.Respond()
	}

	chatStorage, ok := bookStorage[mc.Chat.ID]
	if !ok {
		return c.Respond()
	}
	bookItem, ok := chatStorage[mc.ID]
	if !ok {
		return c.Respond()
	}
	if bookItem.sender != c.Callback().Sender.ID {
		fmt.Println("ID don't match: ", bookItem.sender, c.Callback().Sender.ID)
		return c.Respond(&tele.CallbackResponse{
			Text: "This is not for you, you silly goober",
		})
	}

	page := bookItem.page
	items := bookItem.items
	item := items[conv-1]

	coll := colly.NewCollector(
		colly.Async(true),
	)

	urls := make([]string, 0)
	coll.OnHTML("a", func(e *colly.HTMLElement) {
		if strings.Contains(e.Attr("class"), "js-download-link") {
			if e.Attr("href") != "" {
				urls = append(urls, e.Attr("href"))
			}
		}
	})

	coll.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL.String())
	})

	fullURL := "https://annas-archive.org/" + item.URL
	coll.Visit(fullURL)
	coll.Wait()

	rows := make([]tele.Row, 0)
	rows = append(rows, selector.Row(bookBtnBack))
	fmt.Println("URLS list: ", urls)
	for i, u := range urls {
		// skip URLs that require authentication
		if strings.HasPrefix(u, "/fast_download") {
			continue
		}
		// these URLs require captcha verification
		if strings.HasPrefix(u, "/slow_download") {
			u = "https://annas-archive.org" + u
		}
		if len(rows) > 4 {
			break
		}

		rows = append(rows, selector.Row(selector.URL(fmt.Sprintf("Mirror #%d", i), u)))
	}

	selector.Inline(
		rows...,
	)

	reply := ""
	if item.Title != "" {
		reply = reply + fmt.Sprintf("📎 <b>%s</b>\n\n", html.EscapeString(item.Title))
	}
	if item.Meta != "" {
		reply = reply + fmt.Sprintf("• %s\n", html.EscapeString(item.Meta))
	}

	m, err := c.Bot().Edit(bookItem.message, reply, selector, tele.ModeHTML)
	if err != nil {
		fmt.Println(err)
		return c.Respond()
	}
	saveBookStorageItem(m, items, page, bookItem.sender, bookItem.codes, bookItem.codeMap)

	return c.Respond()
}

func FindBook(query string) ([]*BookItem, error) {
	c := colly.NewCollector(
		colly.Async(true),
	)

	var pageHTML string

	c.OnResponse(func(r *colly.Response) {
		pageHTML = string(r.Body)
	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Println("Visiting", r.URL.String())
	})

	fullURL := "https://annas-archive.org/search?q=" + url.QueryEscape(query)
	err := c.Visit(fullURL)
	if err != nil {
		return nil, err
	}
	c.Wait()

	bookListParsed := make([]*BookItem, 0)

	if pageHTML == "" {
		return bookListParsed, nil
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageHTML))
	if err != nil {
		return nil, err
	}

	normalizeText := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		return strings.Join(strings.Fields(s), " ")
	}

	cleanSelectionText := func(sel *goquery.Selection) string {
		if sel.Length() == 0 {
			return ""
		}
		clone := sel.Clone()
		clone.Find("span").Remove()
		clone.Find("script").Remove()
		return normalizeText(clone.Text())
	}

	extractMeta := func(sel *goquery.Selection) string {
		if sel.Length() == 0 {
			return ""
		}
		clone := sel.Clone()
		clone.Find("a").Remove()
		clone.Find("script").Remove()
		return normalizeText(clone.Text())
	}

	hasIconClass := func(sel *goquery.Selection, class string) bool {
		if strings.Contains(sel.AttrOr("class", ""), class) {
			return true
		}

		found := false
		sel.Find("span").Each(func(_ int, span *goquery.Selection) {
			if found {
				return
			}
			if strings.Contains(span.AttrOr("class", ""), class) {
				found = true
			}
		})
		return found
	}

	doc.Find(".js-aarecord-list-outer > div.flex").Each(func(i int, s *goquery.Selection) {
		details := s.Find("div.max-w-full").First()
		if details.Length() == 0 {
			return
		}

		titleSel := details.Find("a.js-vim-focus").First()
		title := cleanSelectionText(titleSel)
		if title == "" {
			return
		}

		bookURL := titleSel.AttrOr("href", "")
		img := s.Find("img").First().AttrOr("src", "")

		meta := extractMeta(s.Find("div.text-gray-800").First())

		authors := ""
		publisher := ""
		details.Find("a").Each(func(_ int, link *goquery.Selection) {
			switch {
			case authors == "" && hasIconClass(link, "icon-[mdi--user-edit]"):
				authors = cleanSelectionText(link)
			case publisher == "" && hasIconClass(link, "icon-[mdi--company]"):
				publisher = cleanSelectionText(link)
			}
		})

		bookListParsed = append(bookListParsed, &BookItem{
			Meta:      meta,
			Title:     title,
			Publisher: publisher,
			Authors:   authors,
			URL:       bookURL,
			Image:     img,
		})
	})

	return bookListParsed, nil

}
