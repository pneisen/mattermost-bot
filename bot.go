package main

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"

	"github.com/mattermost/platform/model"
)

const (
	botEmail          = "bot@example.com"
	botPassword       = "password1"
	botName           = "samplebot"
	teamName          = "test"
	monitorTownSquare = false
)

func main() {

	client := model.NewClient("http://localhost:8065")
	var botUser *model.User
	var botTeam *model.Team
	var initialLoad *model.InitialLoad
	var channelIDsToIgnore []string

	// Ping the server to make sure we can connect.
	if props, err := client.GetPing(); err != nil {
		fmt.Println("There was a problem pinging the Mattermost server.  Are you sure it's running?")
		fmt.Println(err.Error())
		os.Exit(1)
	} else {
		fmt.Println("Server detected and is running version " + props["version"])
	}

	// lets attempt to login to the Mattermost server as the bot user
	// This will set the token required for all future calls
	// You can get this token with client.AuthToken
	if loginResult, err := client.Login(botName, botPassword); err != nil {
		fmt.Println("There was a problem logging into the Mattermost server.  Are you sure ran the setup steps from the README.md?")
		fmt.Println(err.Error())
		os.Exit(1)
	} else {
		botUser = loginResult.Data.(*model.User)
	}

	// Lets load all the stuff we might need
	if initialLoadResults, err := client.GetInitialLoad(); err != nil {
		fmt.Println("We failed to get the initial load")
		fmt.Println(err.Error())
		os.Exit(1)
	} else {
		initialLoad = initialLoadResults.Data.(*model.InitialLoad)
	}

	// Lets find our bot team
	for _, team := range initialLoad.Teams {
		if team.Name == teamName {
			botTeam = team
			break
		}
	}

	if botTeam == nil {
		fmt.Println("We do not appear to be a member of the team '" + teamName + "'")
		os.Exit(1)
	}

	// This is an important step.  Lets make sure we use the botTeam
	// for all future web service requests that require a team.
	client.SetTeamId(botTeam.Id)

	// Get the channel list we are monitoring. Add town-square to the ignore list if flag is set.
	channelsResult, err := client.GetChannels("")
	if err != nil {
		fmt.Printf("Couldn't get channels: %q\n", err.Error())
	} else {

		fmt.Println("Monitoring these channels:")
		channels := channelsResult.Data.(*model.ChannelList)
		for _, channel := range *channels {
			if channel.Name == "town-square" {
				if monitorTownSquare {
					fmt.Println(channel.Name)
				} else {
					channelIDsToIgnore = append(channelIDsToIgnore, channel.Id)
				}
			} else {
				fmt.Println(channel.Name)
			}
		}
	}

	// Lets start listening to some channels via the websocket!
	webSocketClient, err := model.NewWebSocketClient("ws://localhost:8065", client.AuthToken)
	if err != nil {
		fmt.Println("We failed to connect to the web socket")
		fmt.Println(err.Error())
	}

	webSocketClient.Listen()

	go func() {
		for {
			select {
			case resp := <-webSocketClient.EventChannel:
				handleWebSocket(client, resp, botUser.Id, channelIDsToIgnore)
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	close := make(chan bool, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		for _ = range sig {
			if webSocketClient != nil {
				webSocketClient.Close()
			}

			close <- true
		}
	}()

	<-close
}

func sendReplyMsgToChannel(client *model.Client, msg string, channelID string, replyToID string) {
	post := &model.Post{}
	post.ChannelId = channelID
	post.Message = msg
	post.RootId = replyToID

	if _, err := client.CreatePost(post); err != nil {
		fmt.Println("Failed to send a message to the channel")
		fmt.Println(err.Error())
	}
}

func handleWebSocket(client *model.Client, event *model.WebSocketEvent, userID string, channelIDsToIgnore []string) {
	// Lets only reponded to messaged posted events
	if event.Event != model.WEBSOCKET_EVENT_POSTED {
		return
	}

	post := model.PostFromJson(strings.NewReader(event.Data["post"].(string)))
	if post != nil {

		// ignore my events
		if post.UserId == userID {
			return
		}

		// ignore any ignored channels.
		for _, channelID := range channelIDsToIgnore {
			if channelID == post.ChannelId {
				return
			}
		}

		// Check for ping, reply with PONG
		if matched, _ := regexp.MatchString(`(?:^|\W)ping(?:$|\W)`, post.Message); matched {
			sendReplyMsgToChannel(client, "PONG", post.ChannelId, post.Id)
			return
		}
	}
}
