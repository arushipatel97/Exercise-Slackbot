/* Exercise-bot is a slackbot that periodically (depending on the range chosen)
reminds select members or entire channel to go exercise
(within work hours -> weekdays(8am-6pm)). It makes sure not to repeat messages
two posts in a row. */

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/urfave/cli"
)

type dummyServer struct {
}

func (ds dummyServer) ServeHTTP(http.ResponseWriter, *http.Request) {
}

var timeBetween time.Duration
var slackToken, oauthAccess, botName, channel string
var lowerTime, upperTime int

func main() {
	app := cli.NewApp()
	app.Name = "exercise-slackbot"
	app.Usage = "Slackbot that periodically reminds members to exercise"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "slack-token, s",
			Usage:       "token for slackbot - Bot User OAuth Access Token",
			EnvVar:      "SLACK_TOKEN",
			Destination: &slackToken,
		},
		cli.StringFlag{
			Name:        "oauth-access, o",
			Usage:       "token for slack API - OAuth Access Token",
			EnvVar:      "OAUTH_ACCESS",
			Destination: &oauthAccess,
		},
		cli.StringFlag{
			Name:        "bot-name, b",
			Usage:       "name of bot on slack channel",
			Value:       "exercise-bot",
			Destination: &botName,
		},
		cli.StringFlag{
			Name:        "channel, c",
			Usage:       "name of bot's slack channel",
			EnvVar:      "CHANNEL",
			Destination: &channel,
		},
		cli.IntFlag{
			Name:        "lower-time, l",
			Usage:       "lower bound on time until next post, in minutes",
			Value:       60,
			Destination: &lowerTime,
		},
		cli.IntFlag{
			Name:        "upper-time, u",
			Usage:       "upper bound on time until next post, in minutes",
			Value:       90,
			Destination: &upperTime,
		},
	}
	app.Action = func(c *cli.Context) error {
		run()
		return nil
	}
	err := app.Run(os.Args)
	if err != nil {
		log.Println("error Running cli", err.Error())
	}
}

/* Based on parameters set in main, actually makes API calls and reads from &
   writes to slack channel */
func run() {
	port := os.Getenv("PORT")
	go func(port string) {
		err := http.ListenAndServe(fmt.Sprintf(":%s", port), dummyServer{})
		if err != nil {
			log.Println("error Listen & Serve", err.Error())
		}
	}(port)
	ws, _ := slackConnect(slackToken)
	fmt.Println("Ctrl^C to quit")

	//make call to slack API to get lists of users on channel
	url := fmt.Sprintf("https://slack.com/api/users.list?token=%s", oauthAccess)
	resp, err := http.Get(url)
	if err != nil {
		log.Println("error received for Slack GET", err.Error())
		return
	}

	// error-handling api request failure
	if resp.StatusCode != 200 {
		log.Println("non-200 status code received")
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("error received decoding JSON", err.Error())
		return
	}
	err = resp.Body.Close()
	if err != nil {
		log.Println("error closing body", err.Error())
		return
	}

	var respUser respMembers
	if err = json.Unmarshal(body, &respUser); err != nil {
		log.Println("error on JSON unmarshal", err.Error())
		return
	}

	numUsers := len(respUser.Members)
	if numUsers <= 0 {
		log.Println("error connecting to user api")
		fmt.Println(numUsers)
		return
	}

	var wNum, hNum int //ensures same post not posted twice in a row
	var botPosts int   //number of exercisebot's post to channel
	brokenPipe := false

	//main loop: will continuely run and montior number of messages
	for {
		person, err := personFinder(respUser)
		if err != nil {
			log.Println(err)
		}
		resp := Message{
			Channel: channel,
			Type:    "message",
		}
		resp.Text, wNum = workoutText(person, wNum) //sets actual message
		if lowerTime <= 0 {
			log.Println("invalid lowerTime value")
			return
		}
		if botPosts%(7*24*60/lowerTime) == 0 { //approximately once a week
			resp.Text, hNum = hereText(hNum) //exercise fun fact for everyone
		}
		if correctTime() { //time/day checks-only want during working hours
			err = postMessage(ws, resp)
			if err != nil {
				log.Println("error posting to slack", err.Error())
				brokenPipe = true
				ws, _ = slackConnect(slackToken) //prevent returning from broken pipe
			}
			err = nextPost(lowerTime, upperTime)
			if err != nil {
				log.Println(err.Error())
				return
			}
			if brokenPipe { //post right after connection reset after broken pipe
				timeBetween = 0
				brokenPipe = false
			}
			fmt.Println(timeBetween)
			time.Sleep(timeBetween) //time between post
			botPosts++
		}
	}
}

/* Decides time before next post based on inputted requirements (start & end)*/
func nextPost(timeStart, timeEnd int) (err error) {
	if timeStart >= timeEnd {
		err = fmt.Errorf("Input error, upperTime is smaller than lowerTime")
		return
	}
	next := rand.Intn(timeEnd)
	for next < timeStart {
		next = rand.Intn(timeEnd)
	}
	timeBetween = time.Minute * time.Duration(next)
	return
}

/* Makes sure it is currently a weekday, between working hours(8am-6pm) */
func correctTime() bool {
	now := time.Now()
	hour, _, _ := time.Now().Clock()
	if now.Weekday() >= time.Monday && now.Weekday() <= time.Friday {
		//only weekdays
		if hour >= 12 && hour <= 22 { //8am-6pm
			return true
		}
	}
	return false
}

/* Uses Slack API to make sure that user being mentioned is currently active */
func findPresence(id string) (presence bool, err error) {
	url := fmt.Sprintf("https://slack.com/api/users.getPresence?token=%s&user=%s&pretty=1", oauthAccess, id)
	resp, err := http.Get(url)
	presence = false
	if err != nil {
		log.Println("error received for Slack GET-presence", err.Error())
		return
	}

	// error-handling api request failure
	if resp.StatusCode != 200 {
		log.Println("non-200 status code received-presence")
		return
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("error received decoding JSON-presence", err.Error())
		return
	}
	err = resp.Body.Close()
	if err != nil {
		log.Println("error closing body", err.Error())
		return
	}

	var respPresence respMembersPresence
	err = json.Unmarshal(body, &respPresence)
	if err != nil {
		log.Println("error on JSON unmarshal-presence", err.Error())
		return
	}

	activity := respPresence.Active

	if respPresence.Ok && activity == "active" { //mention only active members
		presence = true
		return
	}
	return
}

/* Takes in all users and randomly finds person to mention based on certain
criteria: not deleted, not last person mentioned, not away, & not a slackbot*/
func personFinder(respUser respMembers) (person string, err error) {
	numUsers := len(respUser.Members)
	if numUsers <= 0 {
		person = "!here"
		fmt.Println(numUsers)
		return
	}
	index := rand.Intn(numUsers)
	active, err := findPresence(respUser.Members[index].Id) //checks activity
	if err != nil {
		log.Println(err)
		return
	}
	for respUser.Members[index].Name == "slackbot" || !active ||
		respUser.Members[index].Deleted || respUser.Members[index].Name == botName {
		index = rand.Intn(numUsers)
		active, err = findPresence(respUser.Members[index].Id)
		if err != nil {
			log.Println(err)
			return
		}
	}
	person = fmt.Sprintf("@%s", respUser.Members[index].Name)
	return
}

/* Takes in the person being mentioned and a number indicating which exercise
message was last posted andomly picks one of 10 exercising statements to post
 to the channel */
func workoutText(person string, prevNum int) (ret string, num int) {
	ret = "Exercise!"
	num = rand.Intn(10)
	for num == prevNum {
		num = rand.Intn(10) //prevents repeating same mesage as last
	}
	switch num {
	case 0:
		ret = fmt.Sprintf("<%s> Time to take a break and go exercise!", person)
	case 1:
		ret = fmt.Sprintf("<%s> Go outside for a walk!", person)
	case 2:
		jumpingjacks := rand.Intn(18) + 2
		ret = fmt.Sprintf("<%s> Go do %d jumping-jacks!", person, jumpingjacks)
	case 3:
		pushups := rand.Intn(8) + 2
		ret = fmt.Sprintf("<%s> Go do %d Push-Ups!", person, pushups)
	case 4:
		ret = fmt.Sprintf("<%s> Relax with some Yoga!", person)
	case 5:
		plank := rand.Intn(58) + 2
		ret = fmt.Sprintf("<%s> Go plank for %d seconds", person, plank)
	case 6:
		wallSit := rand.Intn(28) + 2
		ret = fmt.Sprintf("<%s> Wallsit for %d seconds!", person, wallSit)
	case 7:
		ret = fmt.Sprintf("<%s> If you're sitting and working, raise your desk & stand for the next 45 minutes", person)
	case 8:
		situps := rand.Intn(18) + 2
		ret = fmt.Sprintf("<%s> Go do %d Sit-Ups!", person, situps)
	case 9:
		run := rand.Intn(1) + 1
		if run < 2 {
			ret = fmt.Sprintf("<%s> Run around the office %d time!", person, run)
		} else {
			ret = fmt.Sprintf("<%s> Run around the office %d times!", person, run)
		}
	}
	return ret, num
}

/* Randomly picks one of the 12 exercising fun-facts for everyone
   facts taken from acefitness, thenextweb & livestrong */
func hereText(prev int) (string, int) {
	ret := "Exercise!"
	num := rand.Intn(12)
	for num == prev {
		num = rand.Intn(12) //prevents repeating same mesage as last
	}
	switch num {
	case 0:
		ret = "<!here> Remember, exercise keeps you alert and focused by increasing blood flow to your brain!"
	case 1:
		ret = "<!here> Don't forget, exercise enhances your body’s ability to transfer glucose and oxygen throughout your brain and body, thus increasing your energy level!"
	case 2:
		ret = "<!here> Did you know that a study in the Journal of Experimental Psychology demonstrated that walking indoors and outdoors triggered a burst in creative thinking with the average creative output rising 60 percent when a person was walking?"
	case 3:
		ret = "<!here> Harvard Business Review found that people who managed to stick with their regular exercise routine experienced less trouble finding an optimal work-life balance, possibly because structured activity helped them become better at time management and more confident in their ability to pull off the demands of both work and home. So, go exercise!!"
	case 4:
		ret = "<!here> Get moving! A 9month study of 80 executives showed that exercisers experienced a 22% increase in fitness and a 70% improvement in ability to make complex decisions compared to sedentary peers."
	case 5:
		ret = "<!here> In addition to increasing the ability to focus, think clearly, and learn more effectively, regular exercise improves mood, relieves anxiety and depression, enhances energy, and promotes self-efficacy!"
	case 6:
		ret = "<!here> When you exercise, feel great and believe in yourself, your mindset at work is bound to be optimistic, and that bodes well for job performance — and career growth!"
	case 7:
		ret = "<!here> When you stay physically active, you’re taking care of your body and your brain — reducing health risks and increasing your capacity for learning, motivation, and sharp thinking!"
	case 8:
		ret = "<!here> Remember that regular exercise can help curb feelings of anxiety and depression!"
	case 9:
		ret = "<!here> Regular exercise that includes power walking, running, weight lifting, swimming or jogging can help reduce your risk of developing certain types of illness, such as the common cold!"
	case 10:
		ret = "<!here> A survey found that employees who exercise for at least 30 minutes, three times a week, are 15 percent more likely to have higher job performance!"
	case 11:
		ret = "<!here> According to a survey, absenteeism is 27% lower for those workers who eat healthy and regularly exercise."
	}
	return ret, num
}
