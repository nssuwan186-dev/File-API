package thai

import (
	"strings"
)

type RoomInfo struct {
	Building string `json:"building"`
	Floor    int    `json:"floor"`
	Type     string `json:"type"`
	Price    int    `json:"price"`
	Status   string `json:"status,omitempty"`
}

type SpellingCorrector struct {
	patterns    map[string][]string
	commonWords map[string]int
}

func NewSpellingCorrector() *SpellingCorrector {
	return &SpellingCorrector{
		patterns: map[string][]string{
			"สวัสดี":   {"สวสดี", "สวัดดี", "สวัสดิ", "สะหวัสดี"},
			"รักษ์":    {"รัก", "รัด", "ราด", "ราก"},
			"ประเสริฐ": {"ประเสริช", "ประเสิร์ธ", "ประเสริฐ์"},
			"พรทิพย์":  {"พรทิพ", "พรทิป", "พรธิพย์"},
			"สมชาย":    {"สมชัย", "สมชาย", "สมชาย"},
			"สมหญิง":   {"สมหญิง", "สมหญิ๋ง", "สมยิง"},
			"มานี":     {"มานี", "มานิ", "มะนี"},
			"มีนา":     {"มีนา", "มีนะ", "มินา"},
		},
		commonWords: map[string]int{
			"โรงแรม":    100,
			"ห้องพัก":   90,
			"เช็คอิน":   80,
			"เช็คเอาท์": 80,
			"เงินสด":    70,
			"โอนบัญชี":  70,
		},
	}
}

func (s *SpellingCorrector) Correct(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	for correct, variants := range s.patterns {
		if input == correct {
			return correct
		}
		for _, v := range variants {
			if input == v {
				return correct
			}
		}
	}

	return input
}

func (s *SpellingCorrector) AddPattern(correct string, variants []string) {
	s.patterns[correct] = variants
}

var SimilarChars = map[rune][]rune{
	'ก': {'ถ', 'ภ', 'ค'},
	'ถ': {'ก', 'ภ'},
	'ภ': {'ถ', 'ก'},
	'น': {'ม', 'ห', 'ฮ'},
	'ม': {'น', 'ห'},
	'ห': {'น', 'ม'},
	'ส': {'ข', 'ช'},
	'ข': {'ส', 'ช'},
	'ช': {'ข', 'ส'},
	'บ': {'ป', 'ผ'},
	'ป': {'บ', 'ผ'},
	'ผ': {'บ', 'ป'},
	'ด': {'ต', 'ถ'},
	'ต': {'ด', 'ถ'},
	'ท': {'ห', 'ม'},
	'ย': {'ว', 'ล'},
	'อ': {'ฮ', 'า'},
	'า': {'อ', 'ฮ'},
}

var SimilarNumbers = map[rune][]rune{
	'0': {'O', 'D'},
	'O': {'0', 'D'},
	'D': {'0', 'O'},
	'1': {'I', 'l', '7'},
	'I': {'1', 'l'},
	'l': {'1', 'I'},
	'5': {'S', '6'},
	'S': {'5', '6'},
	'6': {'5', 'S'},
	'8': {'B', '6'},
	'B': {'8', '6'},
}

var TitlePrefixes = []string{
	"นาย", "นาง", "นางสาว", "ดร.", "อาจารย์",
	"Mr.", "Mrs.", "Ms.", "Dr.",
}
