package handlers

import (
	"fmt"
	"log"
	"time"
	"yotei-backend/database"
	"yotei-backend/models"
)

func CheckDeadlinesAndFinalize() error {
	log.Println("Checking and finalizing deadlines...")

	// イベントを取得
	var events []models.Event
	if err := database.DB.Find(&events).Error; err != nil {
		return fmt.Errorf("Failed to get events: %w", err)
	}
	for _, event := range events {
		if event.DeadlineEnable && event.Deadline != nil && event.Deadline.Before(time.Now()) && !event.DeadlineReached {
			log.Println("Event deadline reached:", event.ID)
			decidedCandidateDates, err := mostVotedCandidates(event.ID)
			if err != nil {
				return fmt.Errorf("Failed to decide event schedule: %w", err)
			}

			var rssFeed models.RSSFeed
			if len(decidedCandidateDates) == 0 {
				rssFeed = models.RSSFeed{
					EventID:     event.ID,
					Title:       event.Title,
					Link:        fmt.Sprintf("https://localhost:3000/%s/vote", event.ID),
					Description: fmt.Sprintf("【%s】設定された締切時刻になりましたが、投票がありませんでした。", event.Title),
					CreatedAt:   time.Now(),
				}
			} else if len(decidedCandidateDates) == 1 {
				decidedCandidateDate := decidedCandidateDates[0]
				log.Println("Decided candidate date:", decidedCandidateDate.ID)
				rssFeed = models.RSSFeed{
					EventID:     event.ID,
					Title:       event.Title,
					Link:        fmt.Sprintf("https://localhost:3000/%s/vote", event.ID),
					Description: fmt.Sprintf("【%s】設定された締切時刻になりました。最も投票が多かった予定日はこちらです。\n予定日: %s", event.Title, decidedCandidateDate.DateTime.Format("2006年01月02日")),
					CreatedAt:   time.Now(),
				}
			} else {
				dates := ""
				for i, decidedCandidateDate := range decidedCandidateDates {
					if i != 0 {
						dates += ", "
					}
					dates += decidedCandidateDate.DateTime.Format("2006年01月02日")
				}
				rssFeed = models.RSSFeed{
					EventID:     event.ID,
					Title:       event.Title,
					Link:        fmt.Sprintf("https://localhost:3000/%s/vote", event.ID),
					Description: fmt.Sprintf("【%s】設定された締切時刻になりましたが、最も投票が多かった予定日が複数存在します。\n予定日: %s", event.Title, dates),
					CreatedAt:   time.Now(),
				}
			}

			if err := database.DB.Create(&rssFeed).Error; err != nil {
				return fmt.Errorf("Failed to create RSS feed: %w", err)
			}
			event.DeadlineReached = true
			if err := database.DB.Save(&event).Error; err != nil {
				return fmt.Errorf("Failed to update event: %w", err)
			}
		}
	}

	return nil
}

func CheckAutoDecisionAndFinalize(eventID string) error {
	var event models.Event
	if err := database.DB.First(&event, "id = ?", eventID).Error; err != nil {
		return fmt.Errorf("Failed to get event: %w", err)
	}

	decidedCandidateDates, err := mostVotedCandidates(eventID)
	if err != nil {
		return fmt.Errorf("Failed to get most voted candidates: %w", err)
	}
	if event.AutoDecisionEnable && !event.AutoDecisionReached {
		var rssFeed models.RSSFeed
		if len(decidedCandidateDates) == 0 {
			rssFeed = models.RSSFeed{
				EventID:     eventID,
				Title:       event.Title,
				Link:        fmt.Sprintf("https://localhost:3000/%s/vote", eventID),
				Description: fmt.Sprintf("【%s】%d人以上の投票が集まりましたが、投票日は一日もありませんでした。", event.Title, event.AutoDecisionThreshold),
				CreatedAt:   time.Now(),
			}
		} else if len(decidedCandidateDates) == 1 {
			decidedCandidateDate := decidedCandidateDates[0]
			rssFeed = models.RSSFeed{
				EventID:     eventID,
				Title:       event.Title,
				Link:        fmt.Sprintf("https://localhost:3000/%s/vote", eventID),
				Description: fmt.Sprintf("【%s】%d人以上の投票が集まりました。最も投票が多かった予定日はこちらです。\n予定日: %s", event.Title, event.AutoDecisionThreshold, decidedCandidateDate.DateTime.Format("2006年01月02日")),
				CreatedAt:   time.Now(),
			}
		} else if len(decidedCandidateDates) > 1 {
			dates := ""
			for i, decidedCandidateDate := range decidedCandidateDates {
				if i != 0 {
					dates += ", "
				}
				dates += decidedCandidateDate.DateTime.Format("2006年01月02日")
			}
			rssFeed = models.RSSFeed{
				EventID:     eventID,
				Title:       event.Title,
				Link:        fmt.Sprintf("https://localhost:3000/%s/vote", eventID),
				Description: fmt.Sprintf("【%s】%d人以上の投票が集まりましたが、最も投票が多かった予定日が複数存在します。\n予定日: %s", event.Title, event.AutoDecisionThreshold, dates),
				CreatedAt:   time.Now(),
			}
		}
		if err := database.DB.Create(&rssFeed).Error; err != nil {
			return fmt.Errorf("Failed to create RSS feed: %w", err)
		}
		event.AutoDecisionReached = true
		if err := database.DB.Save(&event).Error; err != nil {
			return fmt.Errorf("Failed to update event: %w", err)
		}
	}

	return nil
}

func mostVotedCandidates(eventID string) ([]models.CandidateDate, error) {
	var event models.Event
	if err := database.DB.Preload("Participants").Preload("CandidateDates").Preload("CandidateDates.Responses").First(&event, "id = ?", eventID).Error; err != nil {
		return []models.CandidateDate{}, fmt.Errorf("Failed to get event: %w", err)
	}

	maxScore := 0
	decidedCandidateDates := []models.CandidateDate{}
	for _, candidateDate := range event.CandidateDates {
		score := 0
		for _, response := range candidateDate.Responses {
			if response.Status == "available" {
				score++
			}
		}
		if score == 0 {
			continue
		}

		if score > maxScore {
			maxScore = score
			decidedCandidateDates = []models.CandidateDate{candidateDate}
		} else if score == maxScore {
			decidedCandidateDates = append(decidedCandidateDates, candidateDate)
		}
	}

	return decidedCandidateDates, nil
}
