FROM golang:1.7-alpine
MAINTAINER Arushi Patel <arushi.patel@namely.com>
WORKDIR /go/src/github.com/rapidloop/exercise-slackbot

ADD exercise-slackbot.go slack.go vendor /go/src/github.com/rapidloop/exercise-slackbot/
ENV SLACK_TOKEN=
ENV OAUTH_ACCESS=
ENV CHANNEL=C5VTQMRHR

RUN apk add --update git && rm -rf /var/cache/apk/*

CMD go run ./exercise-slackbot.go slack.go
