package utils

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"watgbridge/state"

	// register webp for processing twitter's webp og:image
	_ "github.com/watgbridge/webp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.uber.org/zap"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"google.golang.org/protobuf/proto"
)

const (
	MiB                     = 1 << 20
	linkPreviewMaxHTMLSize  = 2 * MiB
	linkPreviewMaxImageSize = 6 * MiB
	linkPreviewTimeout      = 8 * time.Second

	// WhatsApp text carries Markdown delimiters around entities.
	linkPreviewTrim = "?.,\"':!*_~`"

	// These mirror WhatsApp's WebPageInfo encoding: a 140px square inline
	// JPEGThumbnail and a large JPEG capped at 1024px on the long edge.
	previewThumbPx      = 140
	previewThumbQuality = 80
	previewLargePx      = 1024
	previewLargeQuality = 90

	// Refuse decompression bombs before allocating a full RGBA buffer.
	previewMaxSize = 25 * MiB
)

// Ported from Telegram's domain matcher:
// https://github.com/desktop-app/lib_base/blob/9e4b3bd9d69ab8071660e1ef67b78f7bef125d48/base/qthelp_url.cpp#L20-L28
// https://github.com/DrKLO/Telegram/blob/3de37c8cfb039452be7398909461da6288babf15/TMessagesProj/src/main/java/org/telegram/messenger/LinkifyPort.java#L170-L211
var linkPreviewDomainRe = regexp.MustCompile(
	`(?i)(?:([a-z][a-z0-9+.-]*)://)?(?:[\p{L}\p{N}_-]+\.){1,10}[\p{L}\p{N}-]{2,63}(?::\d{1,5})?`,
)

var previewClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: linkPreviewTimeout,
			Control: func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if err != nil {
					return err
				}
				ip, err := netip.ParseAddr(host)
				if err != nil || !publicIP(ip) {
					return fmt.Errorf("refusing non-public address %s", host)
				}
				return nil
			},
		}).DialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// SSRF guard at dial time (covers redirects and rebinding), shared client
var blocked = func() (o []netip.Prefix) {
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32"} {
		o = append(o, netip.MustParsePrefix(s))
	}
	return
}()

func publicIP(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// WaLinkPreview holds the ExtendedTextMessage fields WhatsApp uses to render a
// link preview. WhatsApp does not generate previews on the recipient device; References:
// https://github.com/mautrix/whatsapp/blob/d574a67a2d22a15af72d953ff5c2d44adfe8562c/pkg/msgconv/urlpreview.go#L126-L164
// https://github.com/WhiskeySockets/Baileys/blob/81345beb67ca3fb0eb79a2c20cd4ac24da517929/src/Utils/messages.ts#L405-L423
type WaLinkPreview struct {
	MatchedText   string
	Title         string
	Description   string
	JPEGThumbnail []byte

	ThumbnailDirectPath        string
	ThumbnailMediaKey          []byte
	ThumbnailMediaKeyTimestamp int64
	ThumbnailSHA256            []byte
	ThumbnailEncSHA256         []byte
	ThumbnailWidth             uint32
	ThumbnailHeight            uint32
}

func (p *WaLinkPreview) ApplyTo(msg *waE2E.ExtendedTextMessage) {
	if p == nil || msg == nil {
		return
	}
	msg.MatchedText = proto.String(p.MatchedText)
	if p.Title != "" {
		msg.Title = proto.String(p.Title)
	}
	if p.Description != "" {
		msg.Description = proto.String(p.Description)
	}
	msg.JPEGThumbnail = p.JPEGThumbnail
	msg.PreviewType = waE2E.ExtendedTextMessage_NONE.Enum()
	if p.ThumbnailDirectPath != "" {
		msg.ThumbnailDirectPath = proto.String(p.ThumbnailDirectPath)
		msg.MediaKey = p.ThumbnailMediaKey
		msg.MediaKeyTimestamp = proto.Int64(p.ThumbnailMediaKeyTimestamp)
		msg.ThumbnailSHA256 = p.ThumbnailSHA256
		msg.ThumbnailEncSHA256 = p.ThumbnailEncSHA256
		msg.ThumbnailWidth = proto.Uint32(p.ThumbnailWidth)
		msg.ThumbnailHeight = proto.Uint32(p.ThumbnailHeight)
	}
}

// GenerateWaLinkPreview returns nil when text has no URL or no usable preview.
func GenerateWaLinkPreview(ctx context.Context, text string, waClient *whatsmeow.Client) *WaLinkPreview {
	matched := extractLinkPreviewURL(text)
	if matched == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, linkPreviewTimeout)
	defer cancel()

	body, pageURL, contentType, err := httpGet(ctx, matched, linkPreviewMaxHTMLSize)
	if err != nil {
		if state.State.Logger != nil {
			state.State.Logger.Debug("link preview fetch failed",
				zap.String("url", matched), zap.Error(err))
		}
		return nil
	}

	title, description, imageURL := parseLinkPreview(pageURL, body, contentType)
	title, description = truncateRunes(title, 300), truncateRunes(description, 1000)
	if title == "" && description == "" && imageURL == "" {
		return nil
	}
	preview := &WaLinkPreview{MatchedText: matched, Title: title, Description: description}
	if imageURL == "" {
		return preview
	}

	imageData, _, _, err := httpGet(ctx, imageURL, linkPreviewMaxImageSize)
	if err != nil || !strings.HasPrefix(http.DetectContentType(imageData), "image/") {
		return preview
	}

	// WhatsApp renders the inline preview from JPEGThumbnail and the large card
	// from an uploaded JPEG thumbnail. Its WebPageInfo decodes the source
	// (including WebP), crops the inline one to a 140px square at quality 80,
	// and re-encodes the large one as JPEG at quality 90, capped at 1024px.
	largeJPEG, thumbJPEG, width, height := previewThumbnails(imageData)
	preview.JPEGThumbnail = thumbJPEG
	preview.ThumbnailWidth, preview.ThumbnailHeight = width, height
	if preview.JPEGThumbnail == nil && state.State.Logger != nil && state.State.Config != nil {
		// ffmpeg can still handle formats the Go decoders cannot; title and
		// description alone are already useful if it cannot.
		preview.JPEGThumbnail, _ = GenerateVideoThumbnail(imageData)
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(preview.JPEGThumbnail)); err == nil {
			preview.ThumbnailWidth, preview.ThumbnailHeight = uint32(cfg.Width), uint32(cfg.Height)
		}
	}

	if waClient != nil && len(largeJPEG) > 0 {
		// Large previews need the image uploaded as a link thumbnail; the
		// response fields map directly onto the ExtendedTextMessage fields.
		if upload, err := waClient.Upload(ctx, largeJPEG, whatsmeow.MediaLinkThumbnail); err == nil {
			preview.ThumbnailDirectPath = upload.DirectPath
			preview.ThumbnailMediaKey = upload.MediaKey
			preview.ThumbnailMediaKeyTimestamp = time.Now().Unix()
			preview.ThumbnailSHA256 = upload.FileSHA256
			preview.ThumbnailEncSHA256 = upload.FileEncSHA256
		}
	}
	return preview
}

// previewThumbnails mirrors WhatsApp's WebPageInfo image handling: decode
// the source (WebP included), re-encode the large preview as a JPEG capped at
// 1024px on the long edge, and center-crop a square JPEG for JPEGThumbnail.
// The large dimensions are what ThumbnailWidth/Height describe; using the
// ffmpeg thumbnail's dimensions here made WhatsApp reserve a ~160px card.
func previewThumbnails(data []byte) (large, thumb []byte, width, height uint32) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > previewMaxSize {
		return nil, nil, 0, 0
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, 0, 0
	}
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, nil, 0, 0
	}

	largeW, largeH := srcW, srcH
	if srcW > previewLargePx || srcH > previewLargePx {
		if srcW >= srcH {
			largeW = previewLargePx
			largeH = max(1, srcH*previewLargePx/srcW)
		} else {
			largeH = previewLargePx
			largeW = max(1, srcW*previewLargePx/srcH)
		}
	}
	if encoded := encodePreviewJPEG(scalePreview(src, bounds, largeW, largeH), previewLargeQuality); encoded != nil {
		large = encoded
		width, height = uint32(largeW), uint32(largeH)
	}

	side := min(srcW, srcH)
	if outSide := min(side, previewThumbPx); outSide > 0 {
		x, y := bounds.Min.X+(srcW-side)/2, bounds.Min.Y+(srcH-side)/2
		crop := image.Rect(x, y, x+side, y+side)
		thumb = encodePreviewJPEG(scalePreview(src, crop, outSide, outSide), previewThumbQuality)
	}
	return
}

func encodePreviewJPEG(img image.Image, quality int) []byte {
	if img == nil {
		return nil
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return nil
	}
	// WhatsApp paints JPEG transparency onto white before encoding.
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, canvas.Bounds(), img, bounds.Min, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: quality}); err != nil {
		return nil
	}
	return buf.Bytes()
}

// scalePreview bilinearly resizes src's crop rectangle into a dstW×dstH
// image. This is the Go equivalent of Android's filtered Canvas.drawBitmap.
func scalePreview(src image.Image, crop image.Rectangle, dstW, dstH int) *image.RGBA {
	if src == nil || crop.Dx() <= 0 || crop.Dy() <= 0 || dstW <= 0 || dstH <= 0 {
		return nil
	}
	srcW, srcH := crop.Dx(), crop.Dy()
	source := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	draw.Draw(source, source.Bounds(), src, crop.Min, draw.Src)

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xScale, yScale := float64(srcW)/float64(dstW), float64(srcH)/float64(dstH)
	clamp := func(value, limit int) int {
		if value < 0 {
			return 0
		}
		if value >= limit {
			return limit - 1
		}
		return value
	}
	for y := 0; y < dstH; y++ {
		fy := (float64(y)+0.5)*yScale - 0.5
		y0 := int(math.Floor(fy))
		wy := fy - float64(y0)
		y1 := clamp(y0+1, srcH)
		y0 = clamp(y0, srcH)
		for x := 0; x < dstW; x++ {
			fx := (float64(x)+0.5)*xScale - 0.5
			x0 := int(math.Floor(fx))
			wx := fx - float64(x0)
			x1 := clamp(x0+1, srcW)
			x0 = clamp(x0, srcW)
			c00, c10 := source.RGBAAt(x0, y0), source.RGBAAt(x1, y0)
			c01, c11 := source.RGBAAt(x0, y1), source.RGBAAt(x1, y1)
			dst.SetRGBA(x, y, color.RGBA{
				R: bilinearChannel(c00.R, c10.R, c01.R, c11.R, wx, wy),
				G: bilinearChannel(c00.G, c10.G, c01.G, c11.G, wx, wy),
				B: bilinearChannel(c00.B, c10.B, c01.B, c11.B, wx, wy),
				A: bilinearChannel(c00.A, c10.A, c01.A, c11.A, wx, wy),
			})
		}
	}
	return dst
}

func bilinearChannel(c00, c10, c01, c11 uint8, wx, wy float64) uint8 {
	top := float64(c00)*(1-wx) + float64(c10)*wx
	bottom := float64(c01)*(1-wx) + float64(c11)*wx
	return uint8(math.Round(top*(1-wy) + bottom*wy))
}

func extractLinkPreviewURL(text string) string {
	link := ""
	for _, m := range linkPreviewDomainRe.FindAllStringSubmatchIndex(text, -1) {
		orig, start, end := m[0], m[0], m[1]
		hasScheme := m[2] >= 0
		if !hasScheme {
			// "okay_https://example.com_"
			for start < end && text[start] == '_' {
				start++
			}
			// invalidate cases where domain-like match is present within another string (eg: somebodys_email@example.com)
			if orig > 0 {
				r, _ := utf8.DecodeLastRuneInString(text[:orig])
				if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("@$-_%=./", r) {
					continue
				}
			}
		}
		raw := trimLinkPreview(text[start:linkPreviewTokenEnd(text, end)])
		candidate := raw
		if !hasScheme {
			candidate = "https://" + raw
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Hostname() == "" || !isHTTPScheme(parsed.Scheme) ||
			parsed.User != nil ||
			(!hasScheme && !hasLinkPreviewTLD(parsed.Hostname())) {
			continue
		}
		if hasScheme {
			return raw
		}
		if link == "" {
			link = raw
		}
	}
	return link
}

func isCJKOrFullwidthPunctuation(r rune) bool {
	// CJK Symbols and Punctuation (U+3000 to U+303F)
	isCJKPunctuation := r >= 0x3000 && r <= 0x303F

	// Halfwidth and Fullwidth Forms (U+FF00 to U+FFEF)
	isFullwidthForm := r >= 0xFF00 && r <= 0xFFEF

	return isCJKPunctuation || isFullwidthForm
}

func linkPreviewTokenEnd(text string, from int) int {
	for from < len(text) {
		r, size := utf8.DecodeRuneInString(text[from:])
		if r == 0 || unicode.IsSpace(r) || unicode.IsControl(r) || isCJKOrFullwidthPunctuation(r) {
			// also stop at CJK (or) fullwidth punctuation. eg: for "https://a.com/x，然后" var 'from' does not advance beyond 'x'
			break
		}
		from += size
	}
	return from
}

// trimLinkPreview follows Telegram Desktop's scanner:
// https://github.com/desktop-app/lib_ui/blob/bd7905b40337287076657bc9ea3914ddff6e3570/ui/text/text_entity.cpp#L1820-L1847
// using IsLinkEnd/IsAlmostLinkEnd from:
// https://github.com/desktop-app/lib_ui/blob/bd7905b40337287076657bc9ea3914ddff6e3570/ui/text/text.cpp#L2391-L2413
func trimLinkPreview(raw string) string {
	stack := make([]int, 0, 4)
	for i, r := range raw {
		switch r {
		case '(', '[', '{', '<':
			stack = append(stack, i)
		case ')', ']', '}', '>':
			if len(stack) == 0 {
				return strings.TrimRight(raw[:i], linkPreviewTrim)
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if strings.IndexByte("([{<", raw[open]) != strings.IndexByte(")]}>", byte(r)) {
				return strings.TrimRight(raw[:open], linkPreviewTrim)
			}
		}
	}
	return strings.TrimRight(raw, linkPreviewTrim)
}

// Telegram checks its own top-domain list; this uses the Public Suffix List:
// https://github.com/desktop-app/lib_ui/blob/bd7905b40337287076657bc9ea3914ddff6e3570/ui/text/text_entity.cpp#L192-L196
func hasLinkPreviewTLD(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	i := strings.LastIndexByte(host, '.')
	if i < 0 {
		return false
	}
	tld, err := idna.Lookup.ToASCII(host[i+1:])
	if err != nil {
		return false
	}
	_, icann := publicsuffix.PublicSuffix("foo." + tld)
	return icann
}

// parseLinkPreview extracts the same Open Graph/Twitter metadata Baileys gets
// from link-preview-js:
// https://github.com/WhiskeySockets/Baileys/blob/81345beb67ca3fb0eb79a2c20cd4ac24da517929/src/Utils/link-preview.ts#L33-L111
func parseLinkPreview(base *url.URL, body []byte, contentType string) (title, description, imageURL string) {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		reader = bytes.NewReader(body)
	}
	meta := make(map[string]string)
	var pageTitle string
	var inTitle bool
	tokenizer := xhtml.NewTokenizer(reader)
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			title = coalesceNormalized(meta["og:title"], meta["twitter:title"], pageTitle)
			description = coalesceNormalized(meta["og:description"], meta["twitter:description"], meta["description"])
			imageURL = resolveLinkPreviewURL(base, coalesceNormalized(
				meta["og:image"], meta["og:image:url"], meta["og:image:secure_url"],
				meta["twitter:image"], meta["twitter:image:src"],
			))
			return
		case xhtml.TextToken:
			if inTitle {
				if pageTitle == "" {
					pageTitle = strings.TrimSpace(tokenizer.Token().Data)
				}
				inTitle = false
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			token := tokenizer.Token()
			switch token.Data {
			case "meta":
				var property, nameAttr, content string
				for _, attr := range token.Attr {
					switch attr.Key {
					case "property":
						property = attr.Val
					case "name":
						nameAttr = attr.Val
					case "content":
						content = attr.Val
					}
				}
				if key := strings.ToLower(coalesceNormalized(property, nameAttr)); key != "" && meta[key] == "" {
					meta[key] = strings.TrimSpace(content)
				}
			case "title":
				inTitle = tokenType == xhtml.StartTagToken
			}
		case xhtml.EndTagToken:
			if tokenizer.Token().Data == "title" {
				inTitle = false
			}
		}
	}
}

func resolveLinkPreviewURL(base *url.URL, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Host == "" || !isHTTPScheme(parsed.Scheme) {
		return ""
	}
	return parsed.String()
}

func httpGet(ctx context.Context, rawURL string, maxBytes int64) ([]byte, *url.URL, string, error) {
	lower := strings.ToLower(rawURL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		rawURL = "https://" + rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/*;q=0.8,*/*;q=0.7")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	resp, err := previewClient.Do(req)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, "", fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, nil, "", fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	return body, resp.Request.URL, resp.Header.Get("Content-Type"), nil
}

// Only schemes we can fetch; Telegram also accepts ftp, tg and itmss:
// https://github.com/desktop-app/lib_ui/blob/bd7905b40337287076657bc9ea3914ddff6e3570/ui/text/text_entity.cpp#L179-L189
func isHTTPScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}

func coalesceNormalized(values ...string) string {
	for _, value := range values {
		if value = strings.Join(strings.Fields(value), " "); value != "" {
			return value
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	return string([]rune(value)[:max])
}
