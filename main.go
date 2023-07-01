package main

import (
	"flag"
	"fmt"
	"log"
	"time"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/joho/godotenv"
)

var errr = godotenv.Load()


// Bot parameters
var (
	BotToken       = flag.String("token", os.Getenv("TOKEN"), "Bot access token")
	RemoveCommands = flag.Bool("rmcmd", true, "Remove all commands after shutdowning or not")
)

var s *discordgo.Session

func init() { flag.Parse() }

func init() {
	var err error
	s, err = discordgo.New("Bot " + *BotToken)
	if err != nil {
		log.Fatalf("Invalid bot parameters: %v", err)
	}
}

var (
	integerOptionMinValue          = 1.0
	dmPermission                   = false
	defaultMemberPermissions int64 = discordgo.PermissionManageServer

	commands = []*discordgo.ApplicationCommand{
		{
			Name: "basic-command",
			// すべてのコマンドとオプションには説明が必要です
			// 説明のないコマンド/オプションは登録に失敗します。
			// コマンドの。
			Description: "Basic command",
		},
		{
			Name:        "basic-command-with-files",
			Description: "Basic command with files",
		},
		{
			Name: "record",
			Description: "録音します",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionChannel,
					Name:        "channel-option",
					Description: "チャンネルのオプション",
					// Channel type mask
					ChannelTypes: []discordgo.ChannelType{
						discordgo.ChannelTypeGuildVoice,
					},
					Required: true,
				},
			},
		},
		{
			Name:        "disconnect",
			Description: "ボイスチャンネルから切断",
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"basic-command": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "ちょっと、そこ！ おめでとうございます。最初のスラッシュ コマンドを実行しました。",
				},
			})
		},
		"basic-command-with-files": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "ちょっと、そこ！ おめでとうございます。応答内のファイルを使用して最初のスラッシュ コマンドを実行しました。",
					Files: []*discordgo.File{
						{
							ContentType: "text/plain",
							Name:        "test.txt",
							Reader:      strings.NewReader("Hello Discord!!"),
						},
					},
				},
			})
		},
		"record": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "接続します",
				},
			})

			options := i.ApplicationCommandData().Options

			// Or convert the slice into a map
			optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
			for _, opt := range options {
				optionMap[opt.Name] = opt
				fmt.Println(*&optionMap[opt.Name].Value)
			}
			guildID := i.GuildID
			//channelID := *&optionMap["channel-option"].Value

			userID := i.Member.User.ID
			vs,err := s.State.VoiceState(guildID,userID)
			if err != nil {
				fmt.Println("ボイスチャンネルに参加してください")
				return
			}


			v, err := s.ChannelVoiceJoin(guildID, vs.ChannelID, true, false)

			if err != nil {
				fmt.Println("接続に失敗しました:", err)
				return
			}

			go func() {
				time.Sleep(10 * time.Second)
				close(v.OpusRecv)
				v.Close()
			}()

			handleVoice(v.OpusRecv)
		},
		"disconnect": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "切断します",
				},
			})
			// ボイスチャンネルから切断
			v,err := s.ChannelVoiceJoin(i.GuildID, "", false, true)
			if err != nil {
				fmt.Println("failed to disconnect from voice channel:", err)
				return
			}
			v.Close()

		},
	}
)

func init() {
	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})
}


func createPionRTPPacket(p *discordgo.Packet) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version: 2,
			// Discord の音声ドキュメントから抜粋
			PayloadType:    0x78,
			SequenceNumber: p.Sequence,
			Timestamp:      p.Timestamp,
			SSRC:           p.SSRC,
		},
		Payload: p.Opus,
	}
}

func handleVoice(c chan *discordgo.Packet) {
	files := make(map[uint32]media.Writer)
	for p := range c {
		file, ok := files[p.SSRC]
		if !ok {
			var err error
			file, err = oggwriter.New(fmt.Sprintf("%d.ogg", p.SSRC), 48000, 2)
			if err != nil {
				fmt.Printf("failed to create file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
				return
			}
			files[p.SSRC] = file
		}
		// DiscordGo のタイプから pion RTP パケットを構築します。
		rtp := createPionRTPPacket(p)
		err := file.WriteRTP(rtp)
		if err != nil {
			fmt.Printf("failed to write to file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
		}
	}

	// ここまで到達したら、パケットのリッスンは完了です。 すべてのファイルを閉じます
	for _, f := range files {
		f.Close()
	}
}

func main() {
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})
	err := s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}

	// 所属しているサーバすべてを取得
	guilds, err := s.UserGuilds(100,"","")
	if err != nil {
		fmt.Println("error retrieving guilds:", err)
		return
	}

	log.Println("Adding commands...")

	// 2次元配列で所属しているサーバすべてにスラッシュコマンドを追加する
	registeredCommands := make([][]*discordgo.ApplicationCommand, len(guilds))
	for i:=0; i<len(guilds); i++{
		registeredCommands[i] = make([]*discordgo.ApplicationCommand, len(commands))
	}


	// スラッシュコマンドの作成
	for i, guild := range guilds {
		fmt.Println(guild)
		for j, v := range commands {
			cmd, err := s.ApplicationCommandCreate(s.State.User.ID, guild.ID, v)
			if err != nil {
				log.Panicf("Cannot create '%v' command: %v", v.Name, err)
			}
			registeredCommands[i][j] = cmd
		}
	}

	defer s.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	if *RemoveCommands {
		log.Println("Removing commands...")

		for i, guild := range guilds {
			//fmt.Println(guild)
			for j, v := range registeredCommands {
				command := registeredCommands[i][j]
				err := s.ApplicationCommandDelete(s.State.User.ID, guild.ID, command.ID)
				if err != nil {
					log.Panicf("Cannot delete '%v' command: %v", v[i].Name, err)
				}
			}
		}
	}

	log.Println("Gracefully shutting down.")
}