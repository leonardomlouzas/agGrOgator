package main

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/leonardomlouzas/agGrOgator/internal/config"
	"github.com/leonardomlouzas/agGrOgator/internal/database"
	_ "github.com/lib/pq"
)

type state struct{
	db		*database.Queries
	cfg		*config.Config
}

type command struct {
	name 	string
	args	[]string
}

type commands struct {
	registeredCommands map[string]func(*state, command) error
}

func (c *commands) run(s *state, cmd command) error {
	f, ok := c.registeredCommands[cmd.name]
	if !ok {
		return fmt.Errorf("command not found")
	}
	return f(s, cmd)
}

func (c *commands) register(name string, f func(*state, command) error) {
	c.registeredCommands[name] = f
}


type RSSFeed struct {
	Channel struct {
		Title		string		`xml:"title"`
		Link		string		`xml:"link"`
		Description string		`xml:"description"`
		Item		[]RSSItem	`xml:"item"`
	} `xml:"channel"`
}

type RSSItem struct {
	Title		string		`xml:"title"`
	Link		string		`xml:"link"`
	Description	string		`xml:"description"`
	PubDate		string		`xml:"pubDate"`
}

func main() {
	cfg, err := config.Read()
	if err != nil {
		fmt.Printf("Error: %s\n",err)
		os.Exit(1)
	}

	
	db, err := sql.Open("postgres", cfg.Db_url)
	if err != nil {
		fmt.Printf("Error connecting to database: %s\n", err)
		os.Exit(1)
	}
	defer db.Close()
	
	dbQueries := database.New(db)
	
	newState := &state{
		db:  dbQueries,
		cfg: &cfg,
	}

	cmds := commands{
		registeredCommands: make(map[string]func(*state, command) error),
	}
	cmds.register("login", middlewareConfigInitialized(handlerLogin))
	cmds.register("register", middlewareConfigInitialized(handlerRegister))
	cmds.register("reset", handlerReset)
	cmds.register("users", middlewareConfigInitialized(handlerGetUsers))
	cmds.register("agg", middlewareConfigInitialized(handlerAgg))
	cmds.register("addfeed", middlewareConfigInitialized(middlewareLoggedIn(handlerAddFeed)))
	cmds.register("feeds", middlewareConfigInitialized(middlewareLoggedIn(handlerFeeds)))
	cmds.register("follow", middlewareConfigInitialized(middlewareLoggedIn(handlerFollow)))
	cmds.register("following", middlewareConfigInitialized(middlewareLoggedIn(handlerFollowing)))
	cmds.register("unfollow", middlewareConfigInitialized(middlewareLoggedIn(handlerUnfollow)))
	cmds.register("browse", middlewareConfigInitialized(middlewareLoggedIn(handlerBrowse)))

	if len(os.Args) < 2 {
		fmt.Println("Usage: gator <command> [args...]")
		os.Exit(1)
	}

	cmdName := os.Args[1]
	cmdArgs := os.Args[2:]

	err = cmds.run(newState, command{name: cmdName, args: cmdArgs})
	if err != nil {
		fmt.Printf("Error: %s\n", err)
		os.Exit(1)
	}
}

func handlerLogin(s *state, cmd command) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("username must be the only field and is required")
	}

	if len(cmd.args[0]) == 0 {
		return fmt.Errorf("username cannot be empty")
	}

	name := cmd.args[0]

	_, err := s.db.GetUser(context.Background(), name)
	if err != nil {
		return fmt.Errorf("error retrieving user: %w", err)
	}
	if s.cfg.CurrentUserName == name {
		fmt.Printf("User %s is already logged in\n", name)
		return nil
	}

	err = s.cfg.SetUser(name)
	if err != nil {
		return fmt.Errorf("error setting user: %w", err)
	}
	fmt.Printf("User %s set successfully\n", s.cfg.CurrentUserName)
	return nil
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("username must be the only field and is required")
	}

	if len(cmd.args[0]) == 0 {
		return fmt.Errorf("username cannot be empty")
	}

	name := cmd.args[0]
	user, err := s.db.CreateUser(context.Background(), database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Name:      name,
	})

	if err != nil {
		fmt.Printf("error creating user: %s", err)
		os.Exit(1)
	}

	err = s.cfg.SetUser(name)
	if err != nil {
		return fmt.Errorf("error setting user in config: %w", err)
	}
	fmt.Printf("User %s created successfully with ID %s\n", user.Name, user.ID)
	return nil
}

func handlerGetUsers(s *state, cmd command) error {
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("error retrieving users: %w", err)
	}

	for _, user := range users {
		if user.Name == s.cfg.CurrentUserName {
			fmt.Printf("%s (current)\n", user.Name)
		} else {
			fmt.Printf("%s\n", user.Name)
		}
	}
	return nil
}

func handlerReset(s *state, cmd command) error {
	err := s.db.ResetTable(context.Background())
	if err != nil {
		fmt.Printf("error resetting table: %s", err)
		os.Exit(1)
	}
	fmt.Println("Table reset successfully")
	return nil
}

func fetchFeed(ctx context.Context, feedURL string) (*RSSFeed, error) {
	httpClient := http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("User-Agent", "agGrOgator")	
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error fetching feed: received status code %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var feed RSSFeed
	err = xml.Unmarshal(data, &feed)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling XML: %w", err)
	}

	feed.Channel.Title = html.UnescapeString(feed.Channel.Title)
	feed.Channel.Description = html.UnescapeString(feed.Channel.Description)
	feed.Channel.Link = html.UnescapeString(feed.Channel.Link)
	for i, item := range feed.Channel.Item {
		feed.Channel.Item[i].Title = html.UnescapeString(item.Title)
		feed.Channel.Item[i].Description = html.UnescapeString(item.Description)
		feed.Channel.Item[i].Link = html.UnescapeString(item.Link)
		feed.Channel.Item[i].PubDate = html.UnescapeString(item.PubDate)
	}
	return &feed, nil
}

func handlerAgg(s *state, cmd command) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("feed URL must be the only field and is required")
	}

	if len(cmd.args[0]) == 0 {
		return fmt.Errorf("feed URL cannot be empty")
	}

	timeBetweenRequests, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return fmt.Errorf("invalid time duration: %w", err)
	}

	ticker := time.NewTicker(timeBetweenRequests)

	for ; ; <-ticker.C {
		scrapeFeeds(s)
	}
}

func handlerAddFeed(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 2 {
		return fmt.Errorf("feed name and URL must be provided")
	}

	if len(cmd.args[0]) == 0 || len(cmd.args[1]) == 0 {
		return fmt.Errorf("feed name and URL cannot be empty")
	}

	feedName := cmd.args[0]
	feedURL := cmd.args[1]

	feed, err := s.db.CreateFeed(context.Background(), database.CreateFeedParams{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Name:      feedName,
		Url:       feedURL,
		UserID:    user.ID,
	})
	if err != nil {
		return fmt.Errorf("error creating feed: %w", err)
	}
	fmt.Println("Feed created successfully")

	feed_follow, err := s.db.CreateFeedFollow(context.Background(), database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		return fmt.Errorf("error creating feed follow: %w", err)
	}
	fmt.Printf("User %s now follows feed %s\n", user.Name, feed_follow.FeedName)

	return nil
}

func handlerFeeds(s *state, cmd command, user database.User) error {
	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return fmt.Errorf("error retrieving feeds: %w", err)
	}
	if len(feeds) == 0 {
		fmt.Println("No feeds found")
		return nil
	}
	for _, feed := range feeds {
		fmt.Printf("Name: %s\n", feed.Name)
		fmt.Printf("URL: %s\n", feed.Url)
		fmt.Printf("User ID: %s\n", user.Name)
		fmt.Printf("Updated At: %s\n", feed.UpdatedAt)
		fmt.Println("-----------------------------")
	}
	fmt.Println("Total feeds:", len(feeds))
	return nil
}

func handlerFollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("feed ID must be the only field and is required")
	}

	if len(cmd.args[0]) == 0 {
		return fmt.Errorf("feed ID cannot be empty")
	}

	url := cmd.args[0]
	feed, err := s.db.GetFeedByURL(context.Background(), url)
	if err != nil {
		return fmt.Errorf("error retrieving feed by URL: %w", err)
	}

	feed_follow, err := s.db.CreateFeedFollow(context.Background(), database.CreateFeedFollowParams{
		ID:			uuid.New(),
		CreatedAt:	time.Now(),
		UpdatedAt:	time.Now(),
		UserID:		user.ID,
		FeedID:		feed.ID,
	})
	if err != nil {
		return fmt.Errorf("error creating feed follow: %w", err)
	}
	fmt.Printf("User %s now follows %s\n", feed_follow.UserName, feed_follow.FeedName)
	return nil
}

func handlerFollowing(s *state, cmd command, user database.User) error {
	following, err := s.db.GetFeedFollowsForUser(context.Background(), user.ID)
	if err != nil {
		return fmt.Errorf("error retrieving following feeds: %w", err)
	}

	if len(following) == 0 {
		fmt.Println("You are not following any feeds")
		return nil
	}

	for _, follow := range following {
		fmt.Printf("%s Following feed: %s (ID: %s)\n", user.Name, follow.FeedName, follow.FeedID)
	}
	return nil
}

func handlerUnfollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return fmt.Errorf("feed URL must be the only field and is required")
	}

	if len(cmd.args[0]) == 0 {
		return fmt.Errorf("feed URL cannot be empty")
	}

	url := cmd.args[0]
	feed, err := s.db.GetFeedByURL(context.Background(), url)
	if err != nil {
		return fmt.Errorf("error retrieving feed by URL: %w", err)
	}

	err = s.db.DeleteFeedFollow(context.Background(), database.DeleteFeedFollowParams{
		FeedID: feed.ID,
		UserID: user.ID,
	})
	if err != nil {
		return fmt.Errorf("error unfollowing feed: %w", err)
	}
	
	fmt.Printf("User %s unfollowed feed %s\n", user.Name, feed.Name)
	return nil
}

func scrapeFeeds(s *state) {
	feed, err := s.db.GetNextFeedToFetch(context.Background())
	if err != nil {
		fmt.Printf("Error retrieving next feed to fetch: %s\n", err)
		return
	}

	_, err = s.db.MarkFeedFetched(context.Background(), feed.ID)
	if err != nil {
		fmt.Printf("Error marking feed as fetched: %s\n", err)
		return
	}

	feed_data, err := fetchFeed(context.Background(), feed.Url)
	if err != nil {
		fmt.Printf("Error fetching feed: %s\n", err)
		return
	}

	for _, item := range feed_data.Channel.Item {
		publishedAt := sql.NullTime{}

		if t, err := time.Parse(time.RFC1123Z, item.PubDate); err == nil {
			publishedAt = sql.NullTime{
				Time: t,
				Valid: true,
			}
		}

		_, err = s.db.CreatePost(context.Background(), database.CreatePostParams{
			ID:			uuid.New(),
			CreatedAt:	time.Now(),
			UpdatedAt:	time.Now(),
			FeedID:		feed.ID,
			Title:		item.Title,
			Description:	sql.NullString{
				String: item.Description,
				Valid:  true,
			},
			Url: 		item.Link,
			PublishedAt: publishedAt.Time,

		})
		if err != nil {
			fmt.Printf("Error creating post: %s\n", err)
			continue
		}
	}
	fmt.Printf("Feed %s processed successfully. Found %v posts\n", feed.Name, len(feed_data.Channel.Item))
}

func handlerBrowse(s *state, cmd command, user database.User) error {
	limit := 2
	if len(cmd.args) == 1 {
		if new_limit, err := strconv.Atoi(cmd.args[0]); err == nil {
			limit = new_limit
		} else {
			return fmt.Errorf("invalid limit value: %s", cmd.args[0])
		}
	}

	posts, err := s.db.GetPostsForUser(context.Background(), database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  int32(limit),
	})
	if err != nil {
		return fmt.Errorf("error retrieving posts for user: %w", err)
	}
	if len(posts) == 0 {
		fmt.Println("No posts found")
		return nil
	}

	for _, post := range posts {
		fmt.Printf("Post ID: %s Published at: %s\n", post.ID, post.PublishedAt)
		fmt.Printf("Title: %s\n", post.Title)
		fmt.Printf("Description: %s\n", post.Description.String)
		fmt.Printf("URL: %s\n", post.Url)
		fmt.Println("-----------------------------")
	}
	return nil
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func (*state, command) error {
	return func(s *state, cmd command) error {
		user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
		if err != nil {
			return fmt.Errorf("error retrieving user: %w", err)
		}

		return handler(s, cmd, user)
	}
}

func middlewareConfigInitialized(handler func(s *state, cmd command) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		if s.cfg == nil {
			return fmt.Errorf("config is not initialized")
		}
		return handler(s, cmd)
	}
}