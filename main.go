package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/big"
	"os"
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
		int
	}, 1000)

	go func() {
		epoch := time.Now().Unix()
		fileName := fmt.Sprintf("resc-%d.csv", epoch)
		f, err := os.OpenFile(fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		f.WriteString("tx,max_depth\n")
		format := "%s,%d\n"

		for recv := range ch {
			fmt.Printf("tx: %v, max depth: %v\n", recv.string, recv.int)
			f.WriteString(fmt.Sprintf(format, recv.string, recv.int))
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
		int
	}
}

func new(blockNumber int64, start, end time.Time, limit int, ch chan struct {
	string
	int
}) (replay, error) {

	client, err := ethclient.Dial("https://mainnet.infura.io/v3/c0439b1de7dc42aa981b4da9110350e8")
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
				call, err := r.client.CallContract(context.Background(), callMsg, &blockNumber)

				if err != nil {
					// log.Fatal(err)
					fmt.Printf("err: %v\n", err)
				} else {
					maxDepth := binary.LittleEndian.Uint32(call)
					r.ch <- struct {
						string
						int
					}{tx.Hash().String(), int(maxDepth)}

					r.cnt++
				}
			}

		}
	}
}
