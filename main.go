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
	replay, err := new(blockNumber, *start, *end, limit)
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

func new(blockNumber int64, start, end time.Time, limit int) (replay, error) {

	client, err := ethclient.Dial("http://127.0.0.1:8545")
	if err != nil {
		return replay{}, err
	}
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return replay{}, err
	}
	ch := make(chan struct {
		string
		int
	}, 1000)
	defer close(ch)
	go func() {
		for recv := range ch {
			fmt.Println("########", recv.string, recv.int)
		}

	}()
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
		if len(block.Transactions()) > 0 && blockTime.After(r.start) && blockTime.Before(r.end) {
			if r.cnt > r.limit {
				break
			}
			r.replayTx(*blockNumber, block.Transactions())
		}
		r.blockNumber++
	}
}

func (r *replay) replayTx(blockNumber big.Int, txs types.Transactions) {
	for _, tx := range txs {
		// only contract tx
		if len(tx.Data()) > 0 && tx.To() != nil {
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
				if call, err := r.client.CallContract(context.Background(), callMsg, &blockNumber); err == nil {
					maxDepth := binary.LittleEndian.Uint32(call)
					r.ch <- struct {
						string
						int
					}{tx.Hash().String(), int(maxDepth)}
					fmt.Println(tx.Hash(), maxDepth)
				}
				r.cnt++
			}

		}
	}
}
