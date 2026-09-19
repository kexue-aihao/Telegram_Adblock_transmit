package builtin

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/kexue-aihao/telegram-adblock-transmit/internal/domain"
)

type corpusSample struct {
	ID             string   `json:"id"`
	Family         string   `json:"family"`
	Kind           string   `json:"kind"`
	Text           string   `json:"text"`
	Caption        string   `json:"caption"`
	EntityURL      string   `json:"entity_url"`
	ButtonText     string   `json:"button_text"`
	ButtonURL      string   `json:"button_url"`
	ChannelForward bool     `json:"channel_forward"`
	Want           []string `json:"want"`
}

func TestReviewedCorpus(t *testing.T) {
	checker := New(true)
	families, ids, contents := map[string]string{}, map[string]bool{}, map[string]bool{}
	known := map[string]bool{}
	for _, item := range Catalog() {
		known[item.ID] = true
	}
	for _, split := range []string{"development", "holdout"} {
		t.Run(split, func(t *testing.T) {
			file, err := os.Open("testdata/" + split + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			total, positives, negatives, falseDeletes, missedCategories := 0, 0, 0, 0, 0
			priorityTotal, priorityMatched := 0, 0
			priorityExpectedByID, priorityMatchedByID := map[string]int{}, map[string]int{}
			for scanner.Scan() {
				var sample corpusSample
				if err := json.Unmarshal(scanner.Bytes(), &sample); err != nil {
					t.Fatal(err)
				}
				if sample.ID == "" || ids[sample.ID] || sample.Family == "" ||
					!slices.Contains([]string{"positive", "negative", "adversarial"}, sample.Kind) {
					t.Fatalf("invalid identity/kind: %+v", sample)
				}
				ids[sample.ID] = true
				if previous := families[sample.Family]; previous != "" && previous != split {
					t.Fatalf("template family %s leaked between %s and %s", sample.Family, previous, split)
				}
				families[sample.Family] = split
				contentKey := strings.Join([]string{sample.Text, sample.Caption, sample.EntityURL, sample.ButtonText, sample.ButtonURL, strconv.FormatBool(sample.ChannelForward)}, "\n")
				if contents[contentKey] {
					t.Fatalf("duplicate text/metadata fixture: %s", sample.ID)
				}
				contents[contentKey] = true
				message := msg(sample.Text)
				message.Caption = sample.Caption
				if sample.EntityURL != "" {
					message.Entities = []domain.MessageEntityInfo{{Type: "text_link", URL: sample.EntityURL}}
				}
				if sample.ButtonText != "" || sample.ButtonURL != "" {
					message.InlineButtons = []domain.InlineButtonInfo{{Text: sample.ButtonText, URL: sample.ButtonURL}}
				}
				if sample.ChannelForward {
					message.Forward = &domain.ForwardInfo{Type: "channel"}
				}
				hits := checker.Analyze(message).HitIDs()
				total++
				if len(sample.Want) == 0 {
					negatives++
					if len(hits) > 0 {
						falseDeletes++
						t.Errorf("%s: benign sample hit %v: %s%s", sample.ID, hits, sample.Text, sample.Caption)
					}
					continue
				}
				positives++
				for _, want := range sample.Want {
					if !known[want] {
						t.Fatalf("%s: unknown expected ID %s", sample.ID, want)
					}
					priority := want == HitMoneyLaundering || want == HitMoneyMule || want == HitAphrodisiacTrade
					if priority {
						priorityTotal++
						priorityExpectedByID[want]++
					}
					if !slices.Contains(hits, want) {
						missedCategories++
						t.Errorf("%s: missed %s, got %v: %s%s", sample.ID, want, hits, sample.Text, sample.Caption)
					} else if priority {
						priorityMatched++
						priorityMatchedByID[want]++
					}
				}
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			if total == 0 || positives == 0 || negatives == 0 {
				t.Fatal("corpus split lacks positive/negative coverage")
			}
			t.Logf("synthetic regression: samples=%d advertising=%d benign=%d false_deletions=%d missed_categories=%d priority_hits=%d/%d",
				total, positives, negatives, falseDeletes, missedCategories, priorityMatched, priorityTotal)
			for _, id := range []string{HitMoneyLaundering, HitMoneyMule, HitAphrodisiacTrade} {
				t.Logf("priority %s: matched=%d expected=%d", id, priorityMatchedByID[id], priorityExpectedByID[id])
			}
		})
	}
}
