package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/big"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:        "resc",
		Description: "Replay Ethereum Smart Contracts in a given time period, and output max depth of call stack.",
		Usage:       "history --start 2020-06-01T00:00:00 --end 2021-07-01T00:00:00 limit 10",
		Flags: []cli.Flag{
			&cli.Int64Flag{
				Name:        "block",
				Value:       0,
				DefaultText: "",
			},
			&cli.TimestampFlag{
				Name:   "start",
				Layout: "2006-01-02T15:04:05",
			},
			&cli.TimestampFlag{
				Name:   "end",
				Layout: "2006-01-02T15:04:05",
			},
			&cli.IntFlag{
				Name:     "limit",
				Required: false,
				Value:    100,
			},
		},
		Action: func(c *cli.Context) error {
			resc(c)
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func resc(c *cli.Context) {

	blockNumber := c.Int64("block")
	start := c.Timestamp("start")
	end := c.Timestamp("end")
	limit := c.Int("limit")

	ch := make(chan struct {
		string
		MetadataStats
	}, 1000)

	go func() {
		epoch := time.Now().Unix()
		fileName := fmt.Sprintf("resc-%d.csv", epoch)
		f, err := os.OpenFile(fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		headerFormat := "tx,%s,\n"
		f.WriteString(fmt.Sprintf(headerFormat, csvHeaderStr()))
		format := "%s,%s\n"

		for recv := range ch {
			fmt.Printf("tx: %v, metadata: %v\n", recv.string, recv.MetadataStats)
			metadataStr := recv.MetadataStats.toCsvRow()
			f.WriteString(fmt.Sprintf(format, recv.string, metadataStr))
		}
		f.Close()
	}()
	replay, err := new(blockNumber, *start, *end, limit, ch)
	if err != nil {
		log.Fatal(err)
	}
	replay.traverseBlock()
}

type replay struct {
	blockNumber int64
	start       time.Time
	end         time.Time
	limit       int
	client      ethclient.Client
	chainID     big.Int
	cnt         int
	ch          chan struct {
		string
		MetadataStats
	}
}

func new(blockNumber int64, start, end time.Time, limit int, ch chan struct {
	string
	MetadataStats
}) (replay, error) {

	client, err := ethclient.Dial("http://localhost:8545")
	if err != nil {
		return replay{}, err
	}
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return replay{}, err
	}
	replay := replay{
		blockNumber: blockNumber,
		start:       start,
		end:         end,
		limit:       limit,
		client:      *client,
		chainID:     *chainID,
		cnt:         0,
		ch:          ch,
	}

	return replay, nil
}

func (r *replay) traverseBlock() {

	for {
		blockNumber := big.NewInt(r.blockNumber)
		block, err := r.client.BlockByNumber(context.Background(), blockNumber)
		if err != nil {
			log.Fatal(err)
		}

		blockTimestamp := int64(block.Time())
		blockTime := time.Unix(blockTimestamp, 0)
		fmt.Printf("block: %v, %v\n", block.Number(), blockTime)
		if len(block.Transactions()) > 0 && blockTime.After(r.start) && blockTime.Before(r.end) {
			if r.limit > 0 && r.cnt > r.limit {
				break
			}
			r.replayTx(*blockNumber, block.Transactions())
		}
		r.blockNumber++
	}
}

type CodeStats struct {
	Cnt    int32
	MaxLen int32
	MinLen int32
}
type MetadataStats struct {
	CallDepth         int32
	CreateStats       CodeStats
	Create2Stats      CodeStats
	CallStats         CodeStats
	CallCodeStats     CodeStats
	DelegateCallStats CodeStats
}
type Metadata struct {
	callMaxDepth int32

	createCnt        int32
	createCodeMaxLen int32
	createCodeMinLen int32

	create2Cnt        int32
	create2CodeMaxLen int32
	create2CodeMinLen int32

	callCnt        int32
	callCodeMaxLen int32
	callCodeMinLen int32

	callCodeCnt        int32
	callCodeCodeMaxLen int32
	callCodeCodeMinLen int32

	delegateCodeCnt        int32
	delegateCodeCodeMaxLen int32
	delegateCodeCodeMinLen int32
}

func csvHeaderStr() string {
	metadata := Metadata{}
	v := reflect.ValueOf(metadata)
	typeOfS := v.Type()
	var cols []string
	for i := 0; i < typeOfS.NumField(); i++ {
		cols = append(cols, typeOfS.Field(i).Name)
	}
	return strings.Join(cols, ",")
}

func (stats *MetadataStats) toCsvRow() string {
	cols := []string{
		string(stats.CallDepth),
		string(stats.CreateStats.Cnt),
		string(stats.CreateStats.MaxLen),
		string(stats.CreateStats.MinLen),
		string(stats.Create2Stats.Cnt),
		string(stats.Create2Stats.MaxLen),
		string(stats.Create2Stats.MinLen),
		string(stats.CallStats.Cnt),
		string(stats.CallStats.MaxLen),
		string(stats.CallStats.MinLen),
		string(stats.CallCodeStats.Cnt),
		string(stats.CallCodeStats.MaxLen),
		string(stats.CallCodeStats.MinLen),
		string(stats.DelegateCallStats.Cnt),
		string(stats.DelegateCallStats.MaxLen),
		string(stats.DelegateCallStats.MinLen),
	}
	return strings.Join(cols, ",")
}

func (r *replay) replayTx(blockNumber big.Int, txs types.Transactions) {
	for _, tx := range txs {
		fmt.Printf("tx: %v\n", tx.Hash())
		// only contract tx
		if len(tx.Data()) > 0 && tx.To() != nil {
			fmt.Printf("contract tx: %v\n", tx.Hash())
			if msg, err := tx.AsMessage(types.NewEIP155Signer(&r.chainID), big.NewInt(0)); err == nil {
				callMsg := ethereum.CallMsg{
					From:       msg.From(),
					To:         msg.To(),
					Gas:        msg.Gas(),
					GasPrice:   msg.GasPrice(),
					GasFeeCap:  msg.GasFeeCap(),
					GasTipCap:  msg.GasTipCap(),
					Value:      msg.Value(),
					Data:       msg.Data(),
					AccessList: msg.AccessList(),
				}
				fmt.Printf("to: %v\n", msg.To())
				res, err := r.client.CallContract(context.Background(), callMsg, &blockNumber)

				if err != nil {
					// log.Fatal(err)
					fmt.Printf("err: %v\n", err)
				} else {
					var metadata = MetadataStats{}
					buf := bytes.NewReader(res)
					binary.Read(buf, binary.LittleEndian, &metadata)
					r.ch <- struct {
						string
						MetadataStats
					}{tx.Hash().String(), metadata}

					r.cnt++
				}
			}

		}
	}
}
