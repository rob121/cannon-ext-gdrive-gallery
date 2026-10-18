package main

import (
	"fmt"
	"log"
	"sync"

	"github.com/robfig/cron/v3"
)

type galleryScheduler struct {
	app  *galleryApp
	mu   sync.Mutex
	cron *cron.Cron
}

func newGalleryScheduler(app *galleryApp) *galleryScheduler {
	return &galleryScheduler{app: app}
}

func (s *galleryScheduler) reload() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron != nil {
		s.cron.Stop()
		s.cron = nil
	}

	schedule, err := s.app.loadSchedule()
	if err != nil {
		log.Printf("gdrive-gallery: load schedule: %v", err)
		return
	}
	if !schedule.Enabled {
		log.Printf("gdrive-gallery: scheduled sync disabled")
		return
	}
	minutes := schedule.intervalMinutes()
	expr := fmt.Sprintf("@every %dm", minutes)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parser.Parse(expr); err != nil {
		log.Printf("gdrive-gallery: invalid schedule %q: %v", expr, err)
		return
	}

	c := cron.New(cron.WithParser(parser))
	_, err = c.AddFunc(expr, func() {
		log.Printf("gdrive-gallery: starting scheduled sync")
		if _, _, err := s.app.runSync(); err != nil {
			log.Printf("gdrive-gallery: scheduled sync failed: %v", err)
		}
	})
	if err != nil {
		log.Printf("gdrive-gallery: schedule add: %v", err)
		return
	}
	c.Start()
	s.cron = c
	log.Printf("gdrive-gallery: scheduled sync enabled (every %d minute(s))", minutes)
}
