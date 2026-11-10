package main

import (
	"bufio"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type lyricLine struct {
	at   float64
	text string
}

func loadLyrics(audioPath string) []lyricLine {
	base := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	for _, ext := range []string{".lrc", ".LRC"} {
		path := base + ext
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		lines := parseLyrics(io.LimitReader(file, 1<<20))
		file.Close()
		if len(lines) > 0 {
			return lines
		}
	}
	return nil
}

func parseLyrics(reader io.Reader) []lyricLine {
	var lines []lyricLine
	var offset float64
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		text := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		var times []float64
		for strings.HasPrefix(text, "[") {
			tag, rest, ok := strings.Cut(text[1:], "]")
			if !ok {
				break
			}
			if value, ok := strings.CutPrefix(strings.ToLower(tag), "offset:"); ok {
				if milliseconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
					offset = float64(milliseconds) / 1000
				}
			} else if minutes, seconds, ok := strings.Cut(tag, ":"); ok {
				m, minErr := strconv.Atoi(minutes)
				s, secErr := strconv.ParseFloat(seconds, 64)
				if minErr != nil || secErr != nil || m < 0 || !(s >= 0 && s < 60) {
					break
				}
				times = append(times, float64(m)*60+s)
			} else {
				break
			}
			text = rest
		}
		for _, at := range times {
			lines = append(lines, lyricLine{at, strings.TrimSpace(text)})
		}
	}
	for i := range lines {
		lines[i].at = math.Max(0, lines[i].at+offset)
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].at < lines[j].at })
	return lines
}

func currentLyric(lines []lyricLine, elapsed float64) int {
	return sort.Search(len(lines), func(i int) bool { return lines[i].at > elapsed }) - 1
}
