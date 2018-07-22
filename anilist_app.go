package main

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aqatl/mal/anilist"
	"github.com/aqatl/mal/mal"
	"github.com/fatih/color"
	"github.com/skratchdot/open-golang/open"
	"github.com/urfave/cli"
	"github.com/atotto/clipboard"
)

func AniListApp(app *cli.App) *cli.App {
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "r, refresh",
			Usage: "refreshes cached list",
		},
		cli.IntFlag{
			Name:  "max",
			Usage: "visible entries threshold",
		},
		cli.BoolFlag{
			Name:  "all, a",
			Usage: "display all entries; same as --max -1",
		},
		cli.StringFlag{
			Name: "status",
			Usage: "display entries only with given status " +
				"[watching|planning|completed|repeating|paused|dropped]",
		},
	}

	app.Commands = []cli.Command{
		cli.Command{
			Name:      "mal",
			Aliases:   []string{"s"},
			Usage:     "Switches app mode to MyAnimeList",
			UsageText: "mal mal",
			Action:    switchToMal,
		},
		cli.Command{
			Name:     "eps",
			Aliases:  []string{"episodes"},
			Category: "Update",
			Usage: "Set the watched episodes value. " +
				"If n not specified, the number will be increased by one",
			UsageText: "mal eps <n>",
			Action:    alSetEntryEpisodes,
		},
		cli.Command{
			Name:      "status",
			Category:  "Update",
			Usage:     "Set your status for selected entry",
			UsageText: "mal status [watching|planning|completed|dropped|paused|repeating]",
			Action:    alSetEntryStatus,
		},
		cli.Command{
			Name:      "score",
			Category:  "Update",
			Usage:     "Set your rating for selected entry",
			UsageText: "mal score <0-10>",
			Action:    alSetEntryScore,
		},
		cli.Command{
			Name:      "sel",
			Aliases:   []string{"select"},
			Category:  "Config",
			Usage:     "Select an entry",
			UsageText: "mal sel [entry title]",
			Action:    alSelectEntry,
		},
		cli.Command{
			Name:     "nyaa",
			Aliases:  []string{"n"},
			Category: "Action",
			Usage:    "Open interactive torrent search",
			Action:   alNyaaCui,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "alt",
					Usage: "choose an alternative title",
				},
			},
		},
		cli.Command{
			Name:      "nyaa-web",
			Aliases:   []string{"nw"},
			Category:  "Action",
			Usage:     "Open torrent search in browser",
			UsageText: "mal nyaa-web",
			Action:    alNyaaWebsite,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "alt",
					Usage: "choose an alternative title",
				},
			},
		},
		cli.Command{
			Name:      "web",
			Aliases:   []string{"website", "open", "url"},
			Category:  "Action",
			Usage:     "Open url associated with selected entry or change url if provided",
			UsageText: "mal web <url>",
			Action:    alOpenWebsite,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "clear",
					Usage: "Clear url for current entry",
				},
			},
			Subcommands: []cli.Command{
				cli.Command{
					Name:      "get-all",
					Usage:     "Print all set urls",
					UsageText: "mal web get-all",
					Action:    alPrintWebsites,
				},
			},
		},
		cli.Command{
			Name:      "airing",
			Aliases:   []string{"broadcast"},
			Category:  "Action",
			Usage:     "Print airing time of next episode",
			UsageText: "mal broadcast",
			Action:    alNextAiringEpisode,
		},
		cli.Command{
			Name:      "music",
			Category:  "Action",
			Usage:     "Print opening and ending themes",
			UsageText: "mal music",
			Action:    alPrintMusic,
		},
		cli.Command{
			Name:      "copy",
			Category:  "Action",
			Usage:     "Copy selected value into system clipboard",
			UsageText: "mal copy [title|url]",
			Action:    alCopyIntoClipboard,
		},
	}

	app.Action = cli.ActionFunc(aniListDefaultAction)

	return app
}

func aniListDefaultAction(ctx *cli.Context) error {
	al, err := loadAniList(ctx)
	if err != nil {
		return err
	}
	cfg := LoadConfig()
	status := cfg.ALStatus
	if statusFlag := ctx.String("status"); statusFlag != "" {
		status = anilist.ParseStatus(statusFlag)
	}
	list := alGetList(al, status)

	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt > list[j].UpdatedAt
	})

	var visibleEntries int
	if visibleEntries = ctx.Int("max"); visibleEntries == 0 {
		//`Max` flag not specified, get value from config
		visibleEntries = cfg.MaxVisibleEntries
	}
	if visibleEntries > len(list) || visibleEntries < 0 || ctx.Bool("all") {
		visibleEntries = len(list)
	}

	fmt.Printf("No%64s%8s%6s\n", "Title", "Eps", "Score")
	fmt.Println("================================================================================")
	pattern := "%2d%64.64s%8s%6d\n"
	var entry *anilist.MediaListEntry
	for i := visibleEntries - 1; i >= 0; i-- {
		entry = &list[i]
		if entry.ListId == cfg.ALSelectedID {
			color.HiYellow(pattern, i+1, entry.Title.UserPreferred,
				fmt.Sprintf("%d/%d", entry.Progress, entry.Episodes),
				entry.Score)
		} else {
			fmt.Printf(pattern, i+1, entry.Title.UserPreferred,
				fmt.Sprintf("%d/%d", entry.Progress, entry.Episodes),
				entry.Score)
		}
	}

	return nil
}

func switchToMal(ctx *cli.Context) error {
	appCfg := AppConfig{}
	LoadJsonFile(AppConfigFile, &appCfg)
	appCfg.Mode = MalMode
	if err := SaveJsonFile(AppConfigFile, &appCfg); err != nil {
		return err
	}
	fmt.Println("App mode switched to MyAnimeList")
	return nil
}

func alSetEntryEpisodes(ctx *cli.Context) error {
	al, entry, cfg, err := loadAniListFull(ctx)
	if err != nil {
		return err
	}
	epsBefore := entry.Progress

	if arg := ctx.Args().First(); arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return fmt.Errorf("n must be a non-negative integer")
		}
		if n < 0 {
			return fmt.Errorf("n can't be lower than 0")
		}
		entry.Progress = n
	} else {
		entry.Progress++
	}

	alStatusAutoUpdate(cfg, entry)

	if err = anilist.SaveMediaListEntryWaitAnimation(entry, al.Token); err != nil {
		return err
	}
	if err = saveAniListAnimeLists(al); err != nil {
		return err
	}

	fmt.Println("Updated successfully")
	alPrintEntryDetailsAfterUpdatedEpisodes(entry, epsBefore)
	return nil
}

func alStatusAutoUpdate(cfg *Config, entry *anilist.MediaListEntry) {
	if cfg.StatusAutoUpdateMode == Off || entry.Episodes == 0 {
		return
	}

	if (cfg.StatusAutoUpdateMode == Normal && entry.Progress >= entry.Episodes) ||
		(cfg.StatusAutoUpdateMode == AfterThreshold && entry.Progress > entry.Episodes) {
		entry.Status = anilist.Completed
		entry.Progress = entry.Episodes
		return
	}

	if entry.Status == anilist.Completed && entry.Progress < entry.Episodes {
		entry.Status = anilist.Current
		return
	}
}

func alSetEntryStatus(ctx *cli.Context) error {
	al, entry, _, err := loadAniListFull(ctx)
	if err != nil {
		return err
	}

	status := anilist.ParseStatus(ctx.Args().First())
	if status == anilist.All {
		return fmt.Errorf("invalid status; possible values: " +
			"watching|planning|completed|dropped|paused|repeating")
	}

	entry.Status = status

	if err = anilist.SaveMediaListEntryWaitAnimation(entry, al.Token); err != nil {
		return err
	}
	if err = saveAniListAnimeLists(al); err != nil {
		return err
	}

	fmt.Println("Updated successfully")
	alPrintEntryDetails(entry)
	return nil
}

func alSetEntryScore(ctx *cli.Context) error {
	al, entry, _, err := loadAniListFull(ctx)
	if err != nil {
		return err
	}

	score, err := strconv.Atoi(ctx.Args().First())
	if err != nil || score < 0 || score > 10 {
		return fmt.Errorf("invalid score; valid range: <0;10>")
	}

	entry.Score = score

	if err = anilist.SaveMediaListEntryWaitAnimation(entry, al.Token); err != nil {
		return err
	}
	if err = saveAniListAnimeLists(al); err != nil {
		return err
	}

	fmt.Println("Updated successfully")
	alPrintEntryDetails(entry)
	return nil
}

func alSelectEntry(ctx *cli.Context) error {
	al, err := loadAniList(ctx)
	if err != nil {
		return err
	}
	cfg := LoadConfig()

	searchTerm := strings.ToLower(strings.Join(ctx.Args(), " "))
	if searchTerm == "" {
		return alFuzzySelectEntry(ctx)
	}

	var matchedEntry *anilist.MediaListEntry = nil
	for i, entry := range al.List {
		title := entry.Title.Romaji + " " + entry.Title.English + " " + entry.Title.Native
		if strings.Contains(strings.ToLower(title), searchTerm) {
			if matchedEntry != nil {
				matchedEntry = nil
				break
			}
			matchedEntry = &al.List[i]
		}
	}
	if matchedEntry != nil {
		alSaveSelection(cfg, matchedEntry)
		return nil
	}

	return alFuzzySelectEntry(ctx)
}

func alSaveSelection(cfg *Config, entry *anilist.MediaListEntry) {
	cfg.ALSelectedID = entry.ListId
	cfg.Save()

	fmt.Println("Selected entry:")
	alPrintEntryDetails(entry)
}

func alNyaaWebsite(ctx *cli.Context) error {
	al, err := loadAniList(ctx)
	if err != nil {
		return err
	}
	cfg := LoadConfig()

	entry := al.GetMediaListById(cfg.ALSelectedID)
	if entry == nil {
		return fmt.Errorf("no entry selected")
	}

	var searchTerm string
	if ctx.Bool("alt") {
		fmt.Printf("Select desired title\n\n")
		if searchTerm = chooseStrFromSlice(sliceOfEntryTitles(entry)); searchTerm == "" {
			return fmt.Errorf("no alternative titles")
		}
	} else {
		searchTerm = entry.Title.Romaji
	}

	address := "https://nyaa.si/?f=0&c=1_2&q=" + url.QueryEscape(searchTerm)
	if path := cfg.BrowserPath; path == "" {
		open.Start(address)
	} else {
		open.StartWith(address, path)
	}

	fmt.Println("Searched for:")
	alPrintEntryDetails(entry)
	return nil
}

func alOpenWebsite(ctx *cli.Context) error {
	al, err := loadAniList(ctx)
	if err != nil {
		return nil
	}

	cfg := LoadConfig()

	entry := al.GetMediaListById(cfg.ALSelectedID)
	if entry == nil {
		return fmt.Errorf("no entry selected")
	}

	if newUrl := ctx.Args().First(); newUrl != "" {
		cfg.Websites[entry.IdMal] = newUrl
		cfg.Save()

		fmt.Print("Entry: ")
		color.HiYellow("%v", entry.Title)
		fmt.Print("URL: ")
		color.HiRed("%v", cfg.Websites[entry.IdMal])

		return nil
	}

	if ctx.Bool("clear") {
		delete(cfg.Websites, entry.IdMal)
		cfg.Save()

		fmt.Println("Entry cleared")
		return nil
	}

	if entryUrl, ok := cfg.Websites[entry.IdMal]; ok {
		if path := cfg.BrowserPath; path == "" {
			open.Start(entryUrl)
		} else {
			open.StartWith(entryUrl, path)
		}

		fmt.Println("Opened website for:")
		alPrintEntryDetails(entry)
		fmt.Fprintf(color.Output, "URL: %v\n", color.CyanString("%v", entryUrl))
	} else {
		fmt.Println("Nothing to open")
	}

	return nil
}

func alPrintWebsites(ctx *cli.Context) error {
	al, err := loadAniList(ctx)
	if err != nil {
		return err
	}

	cfg := LoadConfig()

	for k, v := range cfg.Websites {
		entryUrl := fmt.Sprintf("\033[3%d;%dm%s\033[0m ", 3, 1, v)

		var title string
		if entry := al.GetMediaListByMalId(k); entry != nil {
			title = entry.Title.UserPreferred
		}

		fmt.Fprintf(color.Output, "%6d (%s): %s\n", k, title, entryUrl)
	}

	return nil
}

func alNextAiringEpisode(ctx *cli.Context) error {
	al, entry, cfg, err := loadAniListFull(ctx)
	if err != nil {
		return err
	}

	episode := entry.Progress
	if cfg.StatusAutoUpdateMode != AfterThreshold && entry.Progress < entry.Episodes {
		episode++
	}
	schedule, err := anilist.QueryAiringScheduleWaitAnimation(entry.Id, episode, al.Token)
	if err != nil {
		return err
	}

	airingAt := time.Unix(int64(schedule.AiringAt), 0)

	yellow := color.New(color.FgHiYellow).SprintFunc()
	red := color.New(color.FgHiRed).SprintFunc()
	cyan := color.New(color.FgHiCyan).SprintFunc()
	fmt.Fprintf(
		color.Output,
		"Title: %s\n"+
			"Episode: %s\n"+
			"Airing at: %s\n",
		yellow(entry.Title.UserPreferred),
		red(schedule.Episode),
		cyan(airingAt.Format("15:04:05 02-01-2006 MST")),
	)

	tua := schedule.TimeUntilAiring
	if tua < 0 {
		tua *= -1
	}
	timeUntilAiring, err := time.ParseDuration(strconv.Itoa(tua) + "s")
	if err != nil {
		fmt.Println(err)
	} else if schedule.TimeUntilAiring < 0 {
		fmt.Fprintln(color.Output, "Episode aired", cyan(timeUntilAiring), "ago")
	} else {
		fmt.Fprintln(color.Output, "Time until airing:", cyan(timeUntilAiring))
	}
	return nil
}

func alPrintMusic(ctx *cli.Context) error {
	_, entry, _, err := loadAniListFull(ctx)
	if err != nil {
		return err
	}

	details, err := mal.FetchDetailsWithAnimation(&mal.Client{}, &mal.Anime{ID: entry.IdMal})

	printThemes := func(themes []string) {
		for _, theme := range themes {
			fmt.Fprintf(
				color.Output, "  %s\n",
				color.HiYellowString("%s", strings.TrimSpace(theme)))
		}
	}

	fmt.Fprintln(color.Output, "Openings:")
	printThemes(details.OpeningThemes)

	fmt.Fprintln(color.Output, "\nEndings:")
	printThemes(details.EndingThemes)

	return nil
}

func alCopyIntoClipboard(ctx *cli.Context) error {
	_, entry, cfg, err := loadAniListFull(ctx)
	if err != nil {
		return err
	}

	var text string

	switch strings.ToLower(ctx.Args().First()) {
	case "title":
		alts := sliceOfEntryTitles(entry)
		fmt.Printf("Select desired title\n\n")
		if text = chooseStrFromSlice(alts); text == "" {
			return fmt.Errorf("no alternative titles")
		}
	case "url":
		entryUrl, ok := cfg.Websites[entry.IdMal]
		if !ok {
			return fmt.Errorf("no url to copy")
		}
		text = entryUrl
	default:
		return fmt.Errorf("usage: mal copy [title|url]")
	}

	if err = clipboard.WriteAll(text); err == nil {
		fmt.Fprintln(color.Output, "Text", color.HiYellowString("%s", text), "copied into clipboard")
	}

	return err
}
