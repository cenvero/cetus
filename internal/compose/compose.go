package compose

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type Composition struct {
	ID          string
	Width       int
	Height      int
	FPS         int
	Duration    float64
	TotalFrames int
	Clips       []Clip
}

type Clip struct {
	ID         string
	Start      float64
	Duration   float64
	TrackIndex int
	Volume     float64
	Element    string
}

func Parse(path string) (*Composition, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open composition HTML: %w", err)
	}
	defer file.Close()

	doc, err := html.Parse(file)
	if err != nil {
		return nil, fmt.Errorf("parse composition HTML: %w", err)
	}

	roots := findCompositionRoots(doc)
	if len(roots) == 0 {
		return nil, fmt.Errorf("composition root with data-composition-id is required")
	}
	if len(roots) > 1 {
		return nil, fmt.Errorf("exactly one data-composition-id is allowed, found %d", len(roots))
	}

	root := roots[0]
	comp := &Composition{
		ID:       strings.TrimSpace(attr(root, "data-composition-id")),
		FPS:      30,
		Duration: 0,
	}
	if comp.ID == "" {
		return nil, fmt.Errorf("data-composition-id must be non-empty")
	}

	comp.Width, err = requiredPositiveInt(root, "data-width")
	if err != nil {
		return nil, err
	}
	comp.Height, err = requiredPositiveInt(root, "data-height")
	if err != nil {
		return nil, err
	}
	comp.Duration, err = requiredPositiveFloat(root, "data-duration")
	if err != nil {
		return nil, err
	}
	if fpsText := strings.TrimSpace(attr(root, "data-fps")); fpsText != "" {
		comp.FPS, err = parsePositiveInt("data-fps", fpsText)
		if err != nil {
			return nil, err
		}
	}

	comp.Clips, err = parseClips(root, comp.Duration)
	if err != nil {
		return nil, err
	}
	comp.Recalculate()

	return comp, nil
}

func (c *Composition) ApplyOverrides(fps, width, height int) error {
	if fps < 0 {
		return fmt.Errorf("fps must be positive")
	}
	if width < 0 {
		return fmt.Errorf("width must be positive")
	}
	if height < 0 {
		return fmt.Errorf("height must be positive")
	}
	if fps > 0 {
		c.FPS = fps
	}
	if width > 0 {
		c.Width = width
	}
	if height > 0 {
		c.Height = height
	}
	if c.FPS <= 0 {
		return fmt.Errorf("fps must be positive")
	}
	if c.Width <= 0 {
		return fmt.Errorf("width must be positive")
	}
	if c.Height <= 0 {
		return fmt.Errorf("height must be positive")
	}
	c.Recalculate()
	return nil
}

func (c *Composition) Recalculate() {
	c.TotalFrames = int(math.Ceil(c.Duration * float64(c.FPS)))
}

func findCompositionRoots(n *html.Node) []*html.Node {
	var roots []*html.Node
	var walk func(*html.Node)
	walk = func(cur *html.Node) {
		if cur.Type == html.ElementNode && strings.TrimSpace(attr(cur, "data-composition-id")) != "" {
			roots = append(roots, cur)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return roots
}

func parseClips(root *html.Node, compositionDuration float64) ([]Clip, error) {
	var clips []Clip
	var walk func(*html.Node) error
	walk = func(cur *html.Node) error {
		if cur.Type == html.ElementNode && hasClass(cur, "clip") {
			clip, err := parseClip(cur, len(clips), compositionDuration)
			if err != nil {
				return err
			}
			clips = append(clips, clip)
		}
		for child := cur.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return clips, nil
}

func parseClip(n *html.Node, index int, compositionDuration float64) (Clip, error) {
	start, err := requiredNonNegativeFloat(n, "data-start")
	if err != nil {
		return Clip{}, err
	}
	duration, err := requiredPositiveFloat(n, "data-duration")
	if err != nil {
		return Clip{}, err
	}
	track, err := requiredInt(n, "data-track-index")
	if err != nil {
		return Clip{}, err
	}

	volume := 1.0
	if volumeText := strings.TrimSpace(attr(n, "data-volume")); volumeText != "" {
		volume, err = strconv.ParseFloat(volumeText, 64)
		if err != nil {
			return Clip{}, fmt.Errorf("parse data-volume: %w", err)
		}
		if volume < 0 || volume > 1 {
			return Clip{}, fmt.Errorf("data-volume must be between 0.0 and 1.0")
		}
	}

	if start+duration > compositionDuration {
		return Clip{}, fmt.Errorf("clip starting at %.3fs with duration %.3fs exceeds composition duration %.3fs", start, duration, compositionDuration)
	}

	id := strings.TrimSpace(attr(n, "id"))
	if id == "" {
		id = fmt.Sprintf("clip-%d", index)
	}

	return Clip{
		ID:         id,
		Start:      start,
		Duration:   duration,
		TrackIndex: track,
		Volume:     volume,
		Element:    n.Data,
	}, nil
}

func requiredPositiveInt(n *html.Node, name string) (int, error) {
	value, err := requiredAttr(n, name)
	if err != nil {
		return 0, err
	}
	return parsePositiveInt(name, value)
}

func parsePositiveInt(name, value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func requiredInt(n *html.Node, name string) (int, error) {
	value, err := requiredAttr(n, name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return parsed, nil
}

func requiredPositiveFloat(n *html.Node, name string) (float64, error) {
	value, err := requiredAttr(n, name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}

func requiredNonNegativeFloat(n *html.Node, name string) (float64, error) {
	value, err := requiredAttr(n, name)
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must be >= 0", name)
	}
	return parsed, nil
}

func requiredAttr(n *html.Node, name string) (string, error) {
	value := strings.TrimSpace(attr(n, name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, field := range strings.Fields(attr(n, "class")) {
		if field == class {
			return true
		}
	}
	return false
}
