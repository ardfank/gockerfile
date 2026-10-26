package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/moby/moby/client"
)

func convertHistoryEntry(createdBy string) (string, bool) {
	createdBy = strings.TrimSpace(createdBy)
	if createdBy == "" {
		return "", false
	}

	// Remove BuildKit metadata.
	createdBy = strings.TrimSuffix(createdBy, " # buildkit")
	createdBy = strings.TrimSpace(createdBy)

	// Ignore empty-layer metadata that does not represent a Dockerfile instruction.
	if createdBy == "" || createdBy == "/bin/sh -c #(nop) " {
		return "", false
	}

	// Remove the Docker history prefix.
	createdBy = strings.TrimPrefix(createdBy, "RUN ")

	// Convert Docker's generated shell-form RUN commands.
	if strings.HasPrefix(createdBy, "/bin/sh -c ") {
		createdBy = strings.TrimPrefix(createdBy, "/bin/sh -c ")
		createdBy = strings.TrimSpace(createdBy)

		// Skip BuildKit's internal no-op commands.
		if strings.HasPrefix(createdBy, "#(nop)") {
			createdBy = strings.TrimSpace(strings.TrimPrefix(createdBy, "#(nop)"))
			if createdBy == "" {
				return "", false
			}
		} else {
			return "RUN " + createdBy, true
		}
	}

	// Convert EXPOSE output from Go map representation.
	if strings.HasPrefix(createdBy, "EXPOSE map[") {
		start := strings.Index(createdBy, "map[")
		end := strings.LastIndex(createdBy, "]")
		if start < 0 || end <= start+4 {
			return "", false
		}

		ports := strings.Fields(createdBy[start+4 : end])
		if len(ports) == 0 {
			return "", false
		}

		var result []string
		for _, port := range ports {
			port = strings.TrimSuffix(port, ":{}")
			if port == "" {
				continue
			}
			result = append(result, port)
		}

		if len(result) == 0 {
			return "", false
		}

		return "EXPOSE " + strings.Join(result, " "), true
	}

	// Preserve recognized Dockerfile instructions.
	instructions := []string{
		"FROM ", "RUN ", "CMD ", "LABEL ", "MAINTAINER ",
		"EXPOSE ", "ENV ", "ADD ", "COPY ", "ENTRYPOINT ",
		"VOLUME ", "USER ", "WORKDIR ", "ARG ", "ONBUILD ",
		"STOPSIGNAL ", "HEALTHCHECK ", "SHELL ",
	}

	for _, prefix := range instructions {
		if strings.HasPrefix(createdBy, prefix) {
			return createdBy, true
		}
	}

	return "", false
}

func formatRunCommand(command string) string {
	command = strings.TrimSpace(command)

	var result strings.Builder
	indent := "\t"
	depth := 0

	for i := 0; i < len(command); i++ {
		ch := command[i]

		switch ch {
		case '{':
			if i > 0 && command[i-1] == ' ' {
				result.WriteString(" {")
			} else {
				result.WriteByte(ch)
			}
			depth++
			result.WriteString(" \\\n")
			result.WriteString(strings.Repeat(indent, depth))

		case '}':
			result.WriteString(" \\\n")
			if depth > 0 {
				depth--
			}
			result.WriteString(strings.Repeat(indent, depth))
			result.WriteByte(ch)

		case ';':
			result.WriteByte(ch)
			if i+1 < len(command) {
				result.WriteString(" \\\n")
				result.WriteString(strings.Repeat(indent, depth))
			}

		default:
			result.WriteByte(ch)
		}
	}

	return "RUN " + strings.TrimSpace(result.String())
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gf [options] IMAGE\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}

	arg1 := flag.Arg(0)
	arg2 := flag.Arg(1)
	ctx := context.Background()
	cli, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatal(err)
	}
	defer cli.Close()

	if arg1 == "list" {
		list, err := cli.ImageList(ctx, client.ImageListOptions{})
		if err != nil {
			log.Fatal(err)
		}
		for _, image := range list.Items {
			// fmt.Printf("%+v\n", image)
			fmt.Println(image.RepoTags,image.ID, image.Size)
		}
		return
	} else if arg1 == "pull" {
		_, err := cli.ImagePull(ctx, arg2, client.ImagePullOptions{})
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println("Successfully pulled image:", arg2)
		return		
	} else {
		history, err := cli.ImageHistory(ctx, arg1)
		if err != nil {
			log.Fatal(err)
		}
	
		for i := len(history.Items) - 1; i >= 0; i-- {
			h := history.Items[i]
			// fmt.Println(h.CreatedBy)
			if converted, ok := convertHistoryEntry(h.CreatedBy); ok {
				fmt.Println(converted)
			}
		}
		return
	}
}
