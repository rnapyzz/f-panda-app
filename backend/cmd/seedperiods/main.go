// Command seedperiods (idempotently) populates dim_period with a rolling
// window of fiscal periods. It is a standalone, re-runnable tool rather than
// a one-time migration, so the window can be extended in later years by
// simply running it again with updated -from-fy/-to-fy.
//
// Usage: seedperiods [-fiscal-start-month 4] [-from-fy 2024] [-to-fy 2029]
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/rnapyzz/f-panda-app/backend/internal/config"
	"github.com/rnapyzz/f-panda-app/backend/internal/db"
)

func main() {
	fiscalStartMonth := flag.Int("fiscal-start-month", 4, "calendar month (1-12) the fiscal year starts on; 4 = April, the standard Japanese convention")
	fromFY := flag.Int("from-fy", 0, "first fiscal year to seed (default: current fiscal year - 2)")
	toFY := flag.Int("to-fy", 0, "last fiscal year to seed, inclusive (default: current fiscal year + 3)")
	flag.Parse()

	if *fiscalStartMonth < 1 || *fiscalStartMonth > 12 {
		log.Fatal("-fiscal-start-month must be between 1 and 12")
	}

	currentFY := currentFiscalYear(time.Now(), *fiscalStartMonth)
	if *fromFY == 0 {
		*fromFY = currentFY - 2
	}
	if *toFY == 0 {
		*toFY = currentFY + 3
	}
	if *toFY < *fromFY {
		log.Fatal("-to-fy must be >= -from-fy")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	sqlDB, err := sql.Open("mysql", cfg.DBDSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	queries := db.New(sqlDB)
	ctx := context.Background()
	count := 0

	for fy := *fromFY; fy <= *toFY; fy++ {
		for fm := 1; fm <= 12; fm++ {
			calYear, calMonth := calendarYearMonth(fy, fm, *fiscalStartMonth)
			start := time.Date(calYear, time.Month(calMonth), 1, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 1, -1)

			err := queries.UpsertPeriod(ctx, db.UpsertPeriodParams{
				FiscalYear:    int16(fy),
				FiscalMonth:   int8(fm),
				CalendarYear:  int16(calYear),
				CalendarMonth: int8(calMonth),
				StartDate:     start,
				EndDate:       end,
				Label:         fmt.Sprintf("FY%d-%02d", fy, calMonth),
			})
			if err != nil {
				log.Fatalf("upsert period fy=%d fm=%d: %v", fy, fm, err)
			}
			count++
		}
	}

	fmt.Printf("seeded %d periods covering FY%d-FY%d (fiscal start month %d)\n", count, *fromFY, *toFY, *fiscalStartMonth)
}

// currentFiscalYear returns the fiscal year containing t, given the calendar
// month the fiscal year starts on.
func currentFiscalYear(t time.Time, fiscalStartMonth int) int {
	if int(t.Month()) >= fiscalStartMonth {
		return t.Year()
	}
	return t.Year() - 1
}

// calendarYearMonth maps a (fiscal year, fiscal month 1-12) pair to the
// actual (calendar year, calendar month) it falls on.
func calendarYearMonth(fiscalYear, fiscalMonth, fiscalStartMonth int) (int, int) {
	offset := fiscalStartMonth - 1 + fiscalMonth - 1
	calMonth := offset%12 + 1
	calYear := fiscalYear + offset/12
	return calYear, calMonth
}
