package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gochain-io/gochain/v3/accounts/abi"
	"github.com/gochain-io/gochain/v3/common"
	"github.com/gochain-io/gochain/v3/common/hexutil"
	"github.com/gochain-io/gochain/v3/core/types"
	"github.com/gochain-io/gochain/v3/crypto"
	"github.com/gochain-io/web3"
	"github.com/gochain-io/web3/assets"
	"github.com/gochain-io/web3/did"
	"github.com/gochain-io/web3/vc"
	"github.com/urfave/cli"
	"golang.org/x/crypto/sha3"
)

// Flags
var (
	verbose bool
	format  string
)

const (
	asciiLogo = `  ___  _____  ___  _   _    __    ____  _  _ 
 / __)(  _  )/ __)( )_( )  /__\  (_  _)( \( )
( (_-. )(_)(( (__  ) _ (  /(__)\  _)(_  )  ( 
 \___/(_____)\___)(_) (_)(__)(__)(____)(_)\_)`

	pkVarName          = "WEB3_PRIVATE_KEY"
	addrVarName        = "WEB3_ADDRESS"
	networkVarName     = "WEB3_NETWORK"
	rpcURLVarName      = "WEB3_RPC_URL"
	didRegistryVarName = "WEB3_DID_REGISTRY"
)

func main() {
	// Interrupt cancellation.
	ctx, cancelFn := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	defer close(sigCh)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		for range sigCh {
			cancelFn()
		}
	}()

	// Flags
	var netName, rpcUrl, function, contractAddress, toContractAddress, contractFile, privateKey, txFormat, txInputFormat, recepientAddress string
	var amount int
	var testnet, waitForReceipt, upgradeable bool

	app := cli.NewApp()
	app.Name = "web3"
	app.Version = Version
	app.Usage = "web3 cli tool"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "network, n",
			Usage:       `The name of the network. Options: gochain/testnet/ethereum/ropsten/localhost. (default: "gochain")`,
			Destination: &netName,
			EnvVar:      networkVarName,
			Hidden:      false},
		cli.BoolFlag{
			Name:        "testnet",
			Usage:       "Shorthand for '-network testnet'.",
			Destination: &testnet,
			Hidden:      false},
		cli.StringFlag{
			Name:        "rpc-url",
			Usage:       "The network RPC URL",
			Destination: &rpcUrl,
			EnvVar:      rpcURLVarName,
			Hidden:      false},
		cli.BoolFlag{
			Name:        "verbose",
			Usage:       "Enable verbose logging",
			Destination: &verbose,
			Hidden:      false},
		cli.StringFlag{
			Name:        "format, f",
			Usage:       "Output format. Options: json. Default: human readable output.",
			Destination: &format,
			Hidden:      false},
	}
	var network web3.Network
	app.Before = func(*cli.Context) error {
		network = getNetwork(netName, rpcUrl, testnet)
		return nil
	}
	app.Commands = []cli.Command{
		{
			Name:    "block",
			Usage:   "Block details for a block number (decimal integer) or hash (hexadecimal with 0x prefix). Omit for latest.",
			Aliases: []string{"bl"},
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "tx",
					Usage:       "Transaction format: count/hash/detail",
					Destination: &txFormat,
					Value:       "count",
				},
				cli.StringFlag{
					Name:        "input",
					Usage:       "Transaction input data format: len/hex/utf8",
					Destination: &txInputFormat,
					Value:       "len",
				},
			},
			Action: func(c *cli.Context) {
				GetBlockDetails(ctx, network, c.Args().First(), txFormat, txInputFormat)
			},
		},
		{
			Name:    "transaction",
			Aliases: []string{"tx"},
			Usage:   "Transaction details for a tx hash",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "input",
					Usage:       "Transaction input data format: len/hex/utf8",
					Destination: &txInputFormat,
					Value:       "len",
				},
			},
			Action: func(c *cli.Context) {
				GetTransactionDetails(ctx, network, c.Args().First(), txInputFormat)
			},
		},
		{
			Name:    "receipt",
			Aliases: []string{"rc"},
			Usage:   "Transaction receipt for a tx hash",
			Action: func(c *cli.Context) {
				GetTransactionReceipt(ctx, network.URL, c.Args().First(), contractFile)
			},
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "abi",
					Destination: &contractFile,
					Usage:       "ABI file matching deployed contract",
					Hidden:      false},
			},
		},
		{
			Name:    "address",
			Aliases: []string{"addr"},
			Usage:   "Account details for a specific address, or the one corresponding to the private key.",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "private-key, pk",
					Usage:       "The private key",
					EnvVar:      pkVarName,
					Destination: &privateKey,
					Hidden:      false},
			},
			Action: func(c *cli.Context) {
				GetAddressDetails(ctx, network, c.Args().First(), privateKey)
			},
		},
		{
			Name:    "contract",
			Aliases: []string{"c"},
			Usage:   "Contract operations",
			Subcommands: []cli.Command{
				{
					Name:  "build",
					Usage: "Build the specified contract",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  "solc-version, c",
							Usage: "The version of the solc compiler(a tag of the ethereum/solc docker image)",
						},
					},
					Action: func(c *cli.Context) {
						BuildSol(ctx, c.Args().First(), c.String("compiler"))
					},
				},
				{
					Name:  "deploy",
					Usage: "Build and deploy the specified contract to the network",
					Action: func(c *cli.Context) {
						name := c.Args().First()
						tail := c.Args().Tail()
						args := make([]interface{}, len(tail))
						for i, v := range c.Args().Tail() {
							args[i] = v
						}
						DeploySol(ctx, network.URL, privateKey, name, upgradeable, args...)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key, pk",
							Usage:       "The private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
							Hidden:      false},
						cli.BoolFlag{
							Name:        "upgradeable",
							Usage:       "Allow contract to be upgraded",
							Destination: &upgradeable,
							Hidden:      false},
					},
				},
				{
					Name:  "list",
					Usage: "List contract functions",
					Action: func(c *cli.Context) {
						ListContract(contractFile)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "abi",
							Destination: &contractFile,
							Usage:       "The abi file of the deployed contract",
							Hidden:      false},
					},
				},
				{
					Name:  "call",
					Usage: "Call contract function",
					Action: func(c *cli.Context) {
						args := make([]interface{}, len(c.Args()))
						for i, v := range c.Args() {
							args[i] = v
						}
						CallContract(ctx, network.URL, privateKey, contractAddress, contractFile, function, amount, waitForReceipt, args...)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "function",
							Usage:       "Target function name",
							Destination: &function,
							Hidden:      false},
						cli.StringFlag{
							Name:        "address",
							EnvVar:      addrVarName,
							Destination: &contractAddress,
							Usage:       "Deployed contract address",
							Hidden:      false},
						cli.StringFlag{
							Name:        "abi",
							Destination: &contractFile,
							Usage:       "ABI file matching deployed contract",
							Hidden:      false},
						cli.IntFlag{
							Name:        "amount",
							Destination: &amount,
							Usage:       "Amount in wei that you want to send to the transaction",
							Hidden:      false},
						cli.StringFlag{
							Name:        "private-key, pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
							Hidden:      false},
						cli.BoolFlag{
							Name:        "wait",
							Usage:       "Wait for the receipt for transact functions",
							Destination: &waitForReceipt,
							Hidden:      false},
					},
				},
				{
					Name:  "upgrade",
					Usage: "Upgrade contract to new address",
					Action: func(c *cli.Context) {
						UpgradeContract(ctx, network.URL, privateKey, contractAddress, toContractAddress, amount)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "address",
							EnvVar:      addrVarName,
							Destination: &contractAddress,
							Usage:       "Proxy contract address",
							Hidden:      false},
						cli.StringFlag{
							Name:        "to",
							Destination: &toContractAddress,
							Usage:       "Contract address to upgrade to",
							Hidden:      false},
						cli.IntFlag{
							Name:        "amount",
							Destination: &amount,
							Usage:       "Amount in wei that you want to send to the transaction",
							Hidden:      false},
						cli.StringFlag{
							Name:        "private-key",
							Usage:       "Private key",
							EnvVar:      "WEB3_PRIVATE_KEY",
							Destination: &privateKey,
							Hidden:      false},
					},
				},
				{
					Name:  "target",
					Usage: "Return target address of upgradeable proxy",
					Action: func(c *cli.Context) {
						GetTargetContract(ctx, network.URL, contractAddress)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "address",
							EnvVar:      addrVarName,
							Destination: &contractAddress,
							Usage:       "Proxy contract address",
							Hidden:      false},
					},
				},
				{
					Name:  "pause",
					Usage: "Pause an upgradeable contract",
					Action: func(c *cli.Context) {
						address := c.Args().First()
						if address == "" {
							address = contractAddress
						}
						PauseContract(ctx, network.URL, privateKey, address, amount)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "address",
							EnvVar:      addrVarName,
							Destination: &contractAddress,
							Usage:       "Proxy contract address",
							Hidden:      false},
						cli.IntFlag{
							Name:        "amount",
							Destination: &amount,
							Usage:       "Amount in wei that you want to send to the transaction",
							Hidden:      false},
						cli.StringFlag{
							Name:        "private-key",
							Usage:       "Private key",
							EnvVar:      "WEB3_PRIVATE_KEY",
							Destination: &privateKey,
							Hidden:      false},
					},
				},
				{
					Name:  "resume",
					Usage: "Resume a paused upgradeable contract",
					Action: func(c *cli.Context) {
						address := c.Args().First()
						if address == "" {
							address = contractAddress
						}
						ResumeContract(ctx, network.URL, privateKey, address, amount)
					},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "address",
							EnvVar:      addrVarName,
							Destination: &contractAddress,
							Usage:       "Proxy contract address",
							Hidden:      false},
						cli.IntFlag{
							Name:        "amount",
							Destination: &amount,
							Usage:       "Amount in wei that you want to send to the transaction",
							Hidden:      false},
						cli.StringFlag{
							Name:        "private-key",
							Usage:       "Private key",
							EnvVar:      "WEB3_PRIVATE_KEY",
							Destination: &privateKey,
							Hidden:      false},
					},
				},
			},
		},
		{
			Name:    "snapshot",
			Aliases: []string{"sn"},
			Usage:   "Clique snapshot",
			Action: func(c *cli.Context) {
				GetSnapshot(ctx, network.URL)
			},
		},
		{
			Name:    "id",
			Aliases: []string{"id"},
			Usage:   "Network/Chain information",
			Action: func(c *cli.Context) {
				GetID(ctx, network.URL)
			},
		},
		{
			Name:  "start",
			Usage: "Start a local GoChain development node",
			Flags: []cli.Flag{
				cli.BoolTFlag{
					Name:  "detach, d",
					Usage: "Run container in background.",
				},
				cli.StringFlag{
					Name:  "env-file",
					Usage: "Path to custom configuration file.",
				},
				cli.StringFlag{
					Name:   "private-key,pk",
					Usage:  "Private key",
					EnvVar: pkVarName,
				},
			},
			Action: func(c *cli.Context) error {
				return start(ctx, c)
			},
		},
		{
			Name:  "myaddress",
			Usage: fmt.Sprintf("Returns the address associated with %v", pkVarName),
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:   "private-key,pk",
					Usage:  "Private key",
					EnvVar: pkVarName,
				},
			},
			Action: func(c *cli.Context) {
				pk := c.String("private-key")
				if pk == "" {
					fmt.Printf("%v not set", pkVarName)
					return
				}
				acc, err := web3.ParsePrivateKey(pk)
				if err != nil {
					fatalExit(err)
				}
				fmt.Print(acc.PublicKey())
			},
		},
		{
			Name:    "account",
			Aliases: []string{"a"},
			Usage:   "Account operations",
			Subcommands: []cli.Command{
				{
					Name:  "create",
					Usage: "Create a new account",
					Action: func(c *cli.Context) {
						acc, err := web3.CreateAccount()
						if err != nil {
							fatalExit(err)
						}
						fmt.Printf("Private key: %v\n", acc.PrivateKey())
						fmt.Printf("Public address: %v\n", acc.PublicKey())
					},
				},
			},
		},
		{
			Name:    "send",
			Usage:   fmt.Sprintf("Transfer GO to an account (web3 send -to 0xb 10go/eth/nanogo/gwei/attogo/wei)"),
			Aliases: []string{"transfer"},
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:        "private-key,pk",
					Usage:       "Private key",
					EnvVar:      pkVarName,
					Destination: &privateKey,
					Hidden:      false,
				},
				cli.StringFlag{
					Name:        "to",
					EnvVar:      addrVarName,
					Destination: &recepientAddress,
					Usage:       "The recepient address",
					Hidden:      false},
			},
			Action: func(c *cli.Context) {
				Send(ctx, network.URL, privateKey, recepientAddress, c.Args().First())
			},
		},
		{
			Name:  "env",
			Usage: "List environment variables",
			Action: func(c *cli.Context) {
				varNames := []string{addrVarName, pkVarName, networkVarName, rpcURLVarName}
				sort.Strings(varNames)
				for _, name := range varNames {
					fmt.Printf("%s=%s\n", name, os.Getenv(name))
				}
			},
		},
		{
			Name:    "generate",
			Usage:   "Generate a code",
			Aliases: []string{"g"},
			Subcommands: []cli.Command{
				{
					Name:    "contract",
					Usage:   "Generate a contract",
					Aliases: []string{"c"},
					Subcommands: []cli.Command{
						{
							Name:  "erc20",
							Usage: "Generate a erc20 contract",
							Flags: []cli.Flag{
								cli.BoolFlag{
									Name:  "pausable, p",
									Usage: "Pausable contract.",
								},
								cli.BoolTFlag{
									Name:  "mintable, m",
									Usage: "Mintable contract. Default: true",
								},
								cli.BoolTFlag{
									Name:  "burnable, b",
									Usage: "Burnable contract. Default: true",
								},
								cli.StringFlag{
									Name:  "symbol, s",
									Usage: "Token Symbol.",
								},
								cli.StringFlag{
									Name:  "name, n",
									Usage: "Token Name",
								},
								cli.StringFlag{
									Name:  "capped, c",
									Usage: "Cap, total supply(in GO/ETH)",
								},
								cli.IntFlag{
									Name:  "decimals, d",
									Usage: "Decimals",
									Value: 18,
								},
							},
							Action: func(c *cli.Context) {
								GenerateContract(ctx, "erc20", c)
							},
						},
						{
							Name:  "erc721",
							Usage: "Generate a erc721 contract",
							Flags: []cli.Flag{
								cli.BoolFlag{
									Name:  "pausable, p",
									Usage: "Pausable contract.",
								},
								cli.BoolTFlag{
									Name:  "mintable, m",
									Usage: "Mintable contract. Default: true",
								},
								cli.BoolTFlag{
									Name:  "burnable, b",
									Usage: "Burnable contract. Default: true",
								},
								cli.StringFlag{
									Name:  "symbol, s",
									Usage: "Token Symbol.",
								},
								cli.StringFlag{
									Name:  "name, n",
									Usage: "Token Name",
								},
							},
							Action: func(c *cli.Context) {
								GenerateContract(ctx, "erc721", c)
							},
						},
					},
				},
				{
					Name:    "code",
					Usage:   "Generate a code bindings",
					Aliases: []string{"c"},
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:  "abi, a",
							Usage: "Path to the contract ABI json to bind",
						},
						cli.StringFlag{
							Name:  "lang, l",
							Usage: "Destination language for the bindings (go, java, objc)",
							Value: "go",
						},
						cli.StringFlag{
							Name:  "pkg, p",
							Usage: "Package name to generate the binding into.",
							Value: "main",
						},
						cli.StringFlag{
							Name:  "out, o",
							Usage: "Output file for the generated binding (default = main.go).",
							Value: "out.go",
						},
					},
					Action: func(c *cli.Context) {
						GenerateCode(ctx, c)
					},
				},
			},
		},
		{
			Name:    "did",
			Aliases: []string{"c"},
			Usage:   "Distributed identity operations",
			Subcommands: []cli.Command{
				{
					Name:  "create",
					Usage: "Create a new DID",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key,pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
						},
						cli.StringFlag{
							Name:   "registry",
							Usage:  "Registry contract address",
							EnvVar: didRegistryVarName,
						},
					},
					Action: func(c *cli.Context) {
						CreateDID(ctx, network.URL, privateKey, c.Args().First(), c.String("registry"))
					},
				},
				{
					Name:  "owner",
					Usage: "Display DID owner address",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key,pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
						},
						cli.StringFlag{
							Name:   "registry",
							Usage:  "Registry contract address",
							EnvVar: didRegistryVarName,
						},
					},
					Action: func(c *cli.Context) {
						DIDOwner(ctx, network.URL, privateKey, c.Args().First(), c.String("registry"))
					},
				},
				{
					Name:  "hash",
					Usage: "Display DID document IPFS hash",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key,pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
						},
						cli.StringFlag{
							Name:   "registry",
							Usage:  "Registry contract address",
							EnvVar: didRegistryVarName,
						},
					},
					Action: func(c *cli.Context) {
						DIDHash(ctx, network.URL, privateKey, c.Args().First(), c.String("registry"))
					},
				},
				{
					Name:  "show",
					Usage: "Display DID document",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key,pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
						},
						cli.StringFlag{
							Name:   "registry",
							Usage:  "Registry contract address",
							EnvVar: didRegistryVarName,
						},
					},
					Action: func(c *cli.Context) {
						ShowDID(ctx, network.URL, privateKey, c.Args().First(), c.String("registry"))
					},
				},
			},
		},

		{
			Name:    "claim",
			Aliases: []string{"c"},
			Usage:   "Verifiable claims operations",
			Subcommands: []cli.Command{
				{
					Name:  "sign",
					Usage: "Sign a verifiable claim",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key,pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
						},
						cli.StringFlag{
							Name:  "id",
							Usage: "Credential ID",
						},
						cli.StringFlag{
							Name:  "type",
							Usage: "Credential type",
						},
						cli.StringFlag{
							Name:  "issuer",
							Usage: "Credential issuer DID",
						},
						cli.StringFlag{
							Name:  "subject",
							Usage: "Credential subject DID",
						},
						cli.StringFlag{
							Name:  "data",
							Usage: "Credential subject JSON object",
						},
					},
					Action: func(c *cli.Context) {
						SignClaim(ctx, network.URL, privateKey, c.String("id"), c.String("type"), c.String("issuer"), c.String("subject"), c.String("data"))
					},
				},
				{
					Name:  "verify",
					Usage: "Verify a signed claim",
					Flags: []cli.Flag{
						cli.StringFlag{
							Name:        "private-key,pk",
							Usage:       "Private key",
							EnvVar:      pkVarName,
							Destination: &privateKey,
						},
						cli.StringFlag{
							Name:   "registry",
							Usage:  "Registry contract address",
							EnvVar: didRegistryVarName,
						},
					},
					Action: func(c *cli.Context) {
						VerifyClaim(ctx, network.URL, privateKey, c.String("registry"), c.Args().First())
					},
				},
			},
		},
	}
	app.Run(os.Args)
}

// getNetwork resolves the rpcUrl from the user specified options, or quits if an illegal combination or value is found.
func getNetwork(name, rpcURL string, testnet bool) web3.Network {
	var network web3.Network
	if rpcURL != "" {
		if name != "" {
			fatalExit(fmt.Errorf("Cannot set both rpcURL %q and network %q", rpcURL, network))
		}
		if testnet {
			fatalExit(fmt.Errorf("Cannot set both rpcURL %q and testnet", rpcURL))
		}
		network.URL = rpcURL
		network.Unit = "GO"
	} else {
		if testnet {
			if name != "" {
				fatalExit(fmt.Errorf("Cannot set both network %q and testnet", name))
			}
			name = "testnet"
		} else if name == "" {
			name = "gochain"
		}
		var ok bool
		network, ok = web3.Networks[name]
		if !ok {
			fatalExit(fmt.Errorf("Unrecognized network %q", name))
		}
		if verbose {
			log.Printf("Network: %v", name)
		}
	}
	if verbose {
		log.Println("Network Info:", network)
	}
	return network
}

func GetBlockDetails(ctx context.Context, network web3.Network, numberOrHash string, txFormat, txInputFormat string) {
	client, err := web3.Dial(network.URL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", network.URL, err))
	}
	defer client.Close()
	var block *web3.Block
	var includeTxs bool
	switch txFormat {
	case "detail":
		includeTxs = true
	case "count", "hash":
	default:
		fatalExit(fmt.Errorf(`Unrecognized transaction format %q: must be "count", "hash", or "detail"`, txFormat))
	}
	if strings.HasPrefix(numberOrHash, "0x") {
		var err error
		block, err = client.GetBlockByHash(ctx, numberOrHash, includeTxs)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot get block details from the network: %v", err))
		}
	} else {
		var blockN *big.Int
		// Don't try to parse empty string, which means 'latest'.
		if numberOrHash != "" {
			blockN, err = web3.ParseBigInt(numberOrHash)
			if err != nil {
				fatalExit(fmt.Errorf("Block argument must be a number (decimal integer) or hash (hexadecimal with 0x prefix) %q: %v", numberOrHash, err))
			}
		}
		block, err = client.GetBlockByNumber(ctx, blockN, includeTxs)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot get block details from the network: %v", err))
		}
	}
	if verbose {
		log.Println("Block details:")
	}
	switch format {
	case "json":
		fmt.Println(marshalJSON(block))
		return
	}

	fmt.Println("Number:", block.Number)
	fmt.Println("Time:", block.Timestamp.Format(time.RFC3339))
	fmt.Println("Transactions:", block.TxCount())
	gasPct := big.NewRat(int64(block.GasUsed), int64(block.GasLimit))
	gasPct = gasPct.Mul(gasPct, big.NewRat(100, 1))
	fmt.Printf("Gas Used: %d/%d (%s%%)\n", block.GasUsed, block.GasLimit, gasPct.FloatString(2))
	fmt.Println("Difficulty:", block.Difficulty)
	fmt.Println("Total Difficulty:", block.TotalDifficulty)
	fmt.Println("Hash:", block.Hash.String())
	fmt.Println("Vanity:", block.ExtraVanity())
	fmt.Println("Coinbase:", block.Miner.String())
	fmt.Println("ParentHash:", block.ParentHash.String())
	fmt.Println("UncleHash:", block.Sha3Uncles.String())
	fmt.Println("Nonce:", block.Nonce.Uint64())
	fmt.Println("Root:", block.StateRoot.String())
	fmt.Println("TxHash:", block.TxsRoot.String())
	fmt.Println("ReceiptHash:", block.ReceiptsRoot.String())
	fmt.Println("Bloom:", "0x"+common.Bytes2Hex(block.LogsBloom.Bytes()))
	fmt.Println("MixDigest:", block.MixHash.String())
	if len(block.Signers) > 0 {
		fmt.Println("Signers:", fmtAddresses(block.Signers).String())
	}
	if len(block.Voters) > 0 {
		fmt.Println("Voters:", fmtAddresses(block.Voters).String())
	}
	if len(block.Signer) > 0 {
		fmt.Println("Signer:", "0x"+common.Bytes2Hex(block.Signer))
	}
	if block.TxCount() > 0 {
		switch txFormat {
		case "hash":
			fmt.Println("Transaction Hashes:")
			for i, hash := range block.TxHashes {
				fmt.Printf("\t%d\t%s\n", i, hash.Hex())
			}
		case "detail":
			fmt.Println("Transaction Details:")
			for i, tx := range block.TxDetails {
				fmt.Printf("\t%d\t", i)
				fmt.Print("Hash: ", tx.Hash.Hex())
				fmt.Print(" From: ", tx.From.Hex())
				fmt.Print(" To: ", tx.To.Hex())
				fmt.Print(" Value: ", web3.WeiAsBase(tx.Value), " ", network.Unit)
				fmt.Print(" Nonce: ", tx.Nonce)
				fmt.Print(" Gas Limit: ", tx.GasLimit)
				fmt.Print(" Gas Price: ", web3.WeiAsGwei(tx.GasPrice), " gwei")
				fmt.Print(" ")
				printInputData(tx.Input, txInputFormat)
				fmt.Println()
			}
		}
	}
}

type fmtAddresses []common.Address

func (fa fmtAddresses) String() string {
	var b bytes.Buffer
	fmt.Fprint(&b, "[")
	for i, a := range fa {
		if i > 0 {
			fmt.Fprint(&b, ", ")
		}
		fmt.Fprint(&b, a.Hex())
	}
	fmt.Fprint(&b, "]")
	return b.String()
}

func GetTransactionDetails(ctx context.Context, network web3.Network, txhash, inputFormat string) {
	client, err := web3.Dial(network.URL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", network.URL, err))
	}
	defer client.Close()
	tx, err := client.GetTransactionByHash(ctx, common.HexToHash(txhash))
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get transaction details from the network: %v", err))
	}
	if verbose {
		fmt.Println("Transaction details:")
	}

	switch format {
	case "json":
		fmt.Println(marshalJSON(tx))
		return
	}

	fmt.Println("Hash:", tx.Hash.String())
	fmt.Println("From:", tx.From.String())
	if tx.To != nil {
		fmt.Println("To:", tx.To.String())
	}
	fmt.Println("Value:", web3.WeiAsBase(tx.Value), network.Unit)
	fmt.Println("Nonce:", uint64(tx.Nonce))
	fmt.Println("Gas Limit:", tx.GasLimit)
	fmt.Println("Gas Price:", web3.WeiAsGwei(tx.GasPrice), "gwei")
	if tx.BlockHash == (common.Hash{}) {
		fmt.Println("Pending: true")
	} else {
		fmt.Println("Block Number:", tx.BlockNumber)
		fmt.Println("Block Hash:", tx.BlockHash.String())
	}
	printInputData(tx.Input, inputFormat)
	fmt.Println()
}

func printInputData(data []byte, format string) {
	switch format {
	case "len":
		fmt.Print("Input Length: ", len(data), " bytes")
	case "hex":
		fmt.Print("Input: ", hexutil.Encode(data))
	case "utf8":
		fmt.Print("Input: ", string(data))
	default:
		fatalExit(fmt.Errorf(`unrecognized input data format %q: expected "len", "hex", or "utf8"`, format))

	}
}

func GetTransactionReceipt(ctx context.Context, rpcURL, txhash, contractFile string) {
	var myabi *abi.ABI
	client, err := web3.Dial(rpcURL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", rpcURL, err))
	}
	defer client.Close()
	if contractFile != "" {
		myabi = getAbi(contractFile)
	}
	r, err := client.GetTransactionReceipt(ctx, common.HexToHash(txhash))
	if err != nil {
		fatalExit(fmt.Errorf("Failed to get transaction receipt: %v", err))
	}
	if verbose {
		fmt.Println("Transaction Receipt Details:")
	}

	printReceiptDetails(r, myabi)
}

func GetAddressDetails(ctx context.Context, network web3.Network, addrHash, privateKey string) {
	if addrHash == "" {
		if privateKey == "" {
			fatalExit(errors.New("Missing address. Must be specified as only argument, or implied from a private key."))
		}
		acct, err := web3.ParsePrivateKey(privateKey)
		if err != nil {
			fatalExit(err)
		}
		addrHash = acct.PublicKey()
	}
	client, err := web3.Dial(network.URL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", network.URL, err))
	}
	defer client.Close()
	bal, err := client.GetBalance(ctx, addrHash, nil)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get address balance from the network: %v", err))
	}
	code, err := client.GetCode(ctx, addrHash, nil)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get address code from the network: %v", err))
	}
	if verbose {
		log.Println("Address details:")
	}

	switch format {
	case "json":
		data := struct {
			Balance *big.Int `json:"balance"`
			Code    *string  `json:"code"`
		}{Balance: bal}
		if len(code) > 0 {
			sc := string(code)
			data.Code = &sc
		}
		fmt.Println(marshalJSON(&data))
		return
	}

	fmt.Println("Balance:", web3.WeiAsBase(bal), network.Unit)
	if len(code) > 0 {
		fmt.Println("Code:", string(code))
	}
}

func GetSnapshot(ctx context.Context, rpcURL string) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", rpcURL, err))
	}
	defer client.Close()
	s, err := client.GetSnapshot(ctx)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get snapshot from the network: %v", err))
	}
	if verbose {
		log.Println("Snapshot details:")
	}

	switch format {
	case "json":
		fmt.Println(marshalJSON(s))
		return
	}

	fmt.Println("Latest Number:", s.Number)
	fmt.Println("Latest Hash:", s.Hash.String())
	fmt.Println("Signers:")
	type signer struct {
		addr common.Address
		num  uint64
	}
	signers := make([]signer, 0, len(s.Signers))
	for addr, num := range s.Signers {
		signers = append(signers, signer{addr, num})
	}
	sort.Slice(signers, func(i, j int) bool {
		return signers[j].num < signers[i].num
	})
	for _, si := range signers {
		//TODO mark signers which have fallen behind
		fmt.Println("", si.addr.String(), "signed block", si.num, "-", s.Number-si.num, "blocks ago")
	}

	fmt.Println("Voters:")
	for addr := range s.Voters {
		fmt.Println("", addr.String())
	}

	if len(s.Votes) > 0 {
		fmt.Println("Votes:")
		for _, vote := range s.Votes {
			pre := "un"
			if vote.Authorize {
				pre = ""
			}
			fmt.Printf("\t%d: signer %s voted to %sauthorize %s", vote.Block, vote.Signer, pre, vote.Address)
		}
		fmt.Println("Tally:", s.Tally)
		for addr, tally := range s.Tally {
			str := "unauthorize"
			if tally.Authorize {
				str = str[2:]
			}
			fmt.Println("", addr.String(), str, tally.Votes)
		}
	}
}

func GetID(ctx context.Context, rpcURL string) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", rpcURL, err))
	}
	defer client.Close()
	id, err := client.GetID(ctx)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get id info from the network: %v", err))
	}
	if verbose {
		log.Println("Snapshot details:")
	}
	switch format {
	case "json":
		fmt.Println(marshalJSON(id))
		return
	}
	fmt.Println("Network ID:", id.NetworkID)
	fmt.Println("Chain ID:", id.ChainID)
	fmt.Println("Genesis Hash:", id.GenesisHash.String())
}

func BuildSol(ctx context.Context, filename, compiler string) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to read file %q: %v", filename, err))
	}
	str := string(b) // convert content to a 'string'
	if verbose {
		log.Println("Building Sol:", str)
	}
	compileData, err := web3.CompileSolidityString(ctx, str, compiler)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to compile %q: %v", filename, err))
	}
	if verbose {
		log.Println("Compiled Sol Details:", marshalJSON(compileData))
	}

	var filenames []string
	for contractName, v := range compileData {
		fileparts := strings.Split(contractName, ":")
		if fileparts[0] != "<stdin>" {
			continue
		}
		err := ioutil.WriteFile(fileparts[1]+".bin", []byte(v.Code), 0600)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot write the bin file: %v", err))
		}
		err = ioutil.WriteFile(fileparts[1]+".abi", []byte(marshalJSON(v.Info.AbiDefinition)), 0600)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot write the abi file: %v", err))
		}
		filenames = append(filenames, fileparts[1])
	}

	switch format {
	case "json":
		data := struct {
			Bin []string `json:"bin"`
			ABI []string `json:"abi"`
		}{}
		for _, f := range filenames {
			data.Bin = append(data.Bin, f+".bin")
			data.ABI = append(data.ABI, f+".abi")
		}
		fmt.Println(marshalJSON(data))
		return
	}

	fmt.Println("Successfully compiled contracts and wrote the following files:")
	for _, filename := range filenames {
		fmt.Println("", filename+".bin,", filename+".abi")
	}
}

func DeploySol(ctx context.Context, rpcURL, privateKey, contractName string, upgradeable bool, params ...interface{}) {
	if contractName == "" {
		fatalExit(errors.New("Missing contract name arg."))
	}
	client, err := web3.Dial(rpcURL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", rpcURL, err))
	}
	defer client.Close()
	bin, err := ioutil.ReadFile(contractName)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot read the bin file %q: %v", contractName, err))
	}
	var abi string
	if len(params) > 0 {
		abiName := strings.TrimSuffix(contractName, ".bin") + ".abi"
		b, err := ioutil.ReadFile(abiName)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot read the abi file %q: %v", abiName, err))
		}
		abi = string(b)
	}
	tx, err := web3.DeployContract(ctx, client, privateKey, string(bin), abi, params...)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot deploy the contract: %v", err))
	}
	waitCtx, _ := context.WithTimeout(ctx, 60*time.Second)
	receipt, err := web3.WaitForReceipt(waitCtx, client, tx.Hash)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get the receipt: %v", err))
	}

	switch format {
	case "json":
		fmt.Println(marshalJSON(receipt))
		return
	}

	// Exit early if contract is static.
	if !upgradeable {
		fmt.Println("Contract has been successfully deployed with transaction:", tx.Hash.Hex())
		fmt.Println("Contract address is:", receipt.ContractAddress.Hex())
		return
	}

	// Deploy proxy contract.
	proxyTx, err := web3.DeployContract(ctx, client, privateKey, string(assets.OwnerUpgradeableProxyCode(receipt.ContractAddress)), "")
	if err != nil {
		log.Fatalf("Cannot deploy the upgradeable proxy contract: %v", err)
	}
	waitCtx, _ = context.WithTimeout(ctx, 60*time.Second)
	proxyReceipt, err := web3.WaitForReceipt(waitCtx, client, proxyTx.Hash)
	if err != nil {
		log.Fatalf("Cannot get the upgradeable proxy receipt: %v", err)
	}

	fmt.Println("Upgradeable contract has been successfully deployed.")
	fmt.Println("Contract has been successfully deployed with transaction:", proxyTx.Hash.Hex())
	fmt.Println("Contract address is:", proxyReceipt.ContractAddress.Hex())
}

func ListContract(contractFile string) {

	myabi := getAbi(contractFile)

	switch format {
	case "json":
		fmt.Println(marshalJSON(myabi.Methods))
		return
	}

	for _, method := range myabi.Methods {
		fmt.Println(method)
	}

}

func CallContract(ctx context.Context, rpcURL, privateKey, contractAddress, contractFile, functionName string, amount int, waitForReceipt bool, parameters ...interface{}) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", rpcURL, err))
	}
	defer client.Close()
	myabi := getAbi(contractFile)
	if _, ok := myabi.Methods[functionName]; !ok {
		fmt.Println("There is no such function:", functionName)
		return
	}
	if myabi.Methods[functionName].Const {
		res, err := web3.CallConstantFunction(ctx, client, *myabi, contractAddress, functionName, parameters...)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot call the contract: %v", err))
		}
		switch format {
		case "json":
			m := make(map[string]interface{})
			m["response"] = res
			fmt.Println(marshalJSON(m))
			return
		}
		fmt.Println(res)
		return
	}
	tx, err := web3.CallTransactFunction(ctx, client, *myabi, contractAddress, privateKey, functionName, amount, parameters...)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot call the contract: %v", err))
	}
	if !waitForReceipt {
		fmt.Println("Transaction address:", tx.Hash.Hex())
		return
	}
	ctx, _ = context.WithTimeout(ctx, 10*time.Second)
	receipt, err := web3.WaitForReceipt(ctx, client, tx.Hash)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get the receipt: %v", err))
	}
	printReceiptDetails(receipt, myabi)

}

func Send(ctx context.Context, rpcURL, privateKey, toAddress, amount string) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		fatalExit(fmt.Errorf("Failed to connect to %q: %v", rpcURL, err))
	}
	defer client.Close()
	nAmount, err := web3.ParseAmount(amount)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot parse amount: %v", err))
	}
	if toAddress == "" {
		fatalExit(errors.New("The recepient address cannot be empty"))
	}
	address := common.HexToAddress(toAddress)
	tx, err := web3.Send(ctx, client, privateKey, address, nAmount)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot create transaction: %v", err))
	}
	fmt.Println("Transaction address:", tx.Hash.Hex())
}

func printReceiptDetails(r *web3.Receipt, myabi *abi.ABI) {
	var logs []web3.Event
	var err error
	if myabi != nil {
		logs, err = web3.ParseLogs(*myabi, r.Logs)
		r.ParsedLogs = logs
		if err != nil {
			fatalExit(fmt.Errorf("Cannot parse the receipt logs: %v", err))
		}
	}
	switch format {
	case "json":
		fmt.Println(marshalJSON(r))
		return
	}

	fmt.Println("Transaction receipt address:", r.TxHash.Hex())
	fmt.Printf("Block: #%d %s\n", r.BlockNumber, r.BlockHash.Hex())
	fmt.Println("Tx Index:", r.TxIndex)
	fmt.Println("Tx Hash:", r.TxHash.String())
	fmt.Println("From:", r.From.Hex())
	if r.To != nil {
		fmt.Println("To:", r.To.Hex())
	}
	if r.ContractAddress != (common.Address{}) {
		fmt.Println("Contract Address:", r.ContractAddress.String())
	}
	fmt.Println("Gas Used:", r.GasUsed)
	fmt.Println("Cumulative Gas Used:", r.CumulativeGasUsed)
	var status string
	switch r.Status {
	case types.ReceiptStatusFailed:
		status = "Failed"
	case types.ReceiptStatusSuccessful:
		status = "Successful"
	default:
		status = fmt.Sprintf("%d (unrecognized status)", r.Status)
	}
	fmt.Println("Status:", status)
	fmt.Println("Post State:", "0x"+common.Bytes2Hex(r.PostState))
	fmt.Println("Bloom:", "0x"+common.Bytes2Hex(r.Bloom.Bytes()))
	fmt.Println("Logs:", r.Logs)
	if myabi != nil {
		fmt.Println("Parsed Logs:", marshalJSON(r.ParsedLogs))
	}
}

func UpgradeContract(ctx context.Context, rpcURL, privateKey, contractAddress, newTargetAddress string, amount int) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()
	myabi, err := abi.JSON(strings.NewReader(assets.UpgradeableProxyABI))
	if err != nil {
		log.Fatalf("Cannot initialize ABI: %v", err)
	}
	tx, err := web3.CallTransactFunction(ctx, client, myabi, contractAddress, privateKey, "upgrade", amount, newTargetAddress)
	if err != nil {
		log.Fatalf("Cannot upgrade the contract: %v", err)
	}
	ctx, _ = context.WithTimeout(ctx, 60*time.Second)
	receipt, err := web3.WaitForReceipt(ctx, client, tx.Hash)
	if err != nil {
		log.Fatalf("Cannot get the receipt: %v", err)
	}
	fmt.Println("Transaction address:", receipt.TxHash.Hex())
}

func GetTargetContract(ctx context.Context, rpcURL, contractAddress string) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()
	myabi, err := abi.JSON(strings.NewReader(assets.UpgradeableProxyABI))
	if err != nil {
		log.Fatalf("Cannot initialize ABI: %v", err)
	}
	res, err := web3.CallConstantFunction(ctx, client, myabi, contractAddress, "target")
	if err != nil {
		log.Fatalf("Cannot upgrade the contract: %v", err)
	}
	switch res := res.(type) {
	case common.Address:
		fmt.Println(res.String())
	default:
		log.Fatalf("Unexpected return: %#v", res)
	}
}

func PauseContract(ctx context.Context, rpcURL, privateKey, contractAddress string, amount int) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()
	myabi, err := abi.JSON(strings.NewReader(assets.UpgradeableProxyABI))
	if err != nil {
		log.Fatalf("Cannot initialize ABI: %v", err)
	}
	tx, err := web3.CallTransactFunction(ctx, client, myabi, contractAddress, privateKey, "pause", amount)
	if err != nil {
		log.Fatalf("Cannot pause the contract: %v", err)
	}
	ctx, _ = context.WithTimeout(ctx, 60*time.Second)
	receipt, err := web3.WaitForReceipt(ctx, client, tx.Hash)
	if err != nil {
		log.Fatalf("Cannot get the receipt: %v", err)
	}
	fmt.Println("Transaction address:", receipt.TxHash.Hex())
}

func ResumeContract(ctx context.Context, rpcURL, privateKey, contractAddress string, amount int) {
	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()
	myabi, err := abi.JSON(strings.NewReader(assets.UpgradeableProxyABI))
	if err != nil {
		log.Fatalf("Cannot initialize ABI: %v", err)
	}
	tx, err := web3.CallTransactFunction(ctx, client, myabi, contractAddress, privateKey, "resume", amount)
	if err != nil {
		log.Fatalf("Cannot resume the contract: %v", err)
	}
	ctx, _ = context.WithTimeout(ctx, 60*time.Second)
	receipt, err := web3.WaitForReceipt(ctx, client, tx.Hash)
	if err != nil {
		log.Fatalf("Cannot get the receipt: %v", err)
	}
	fmt.Println("Transaction address:", receipt.TxHash.Hex())
}

// MaxDIDLength is the maximum size of the idstring of the GoChain DID.
const MaxDIDLength = 32

func CreateDID(ctx context.Context, rpcURL, privateKey, id, registryAddress string) {
	if registryAddress == "" {
		log.Fatalf("Registry contract address required")
	} else if id == "" {
		log.Fatalf("DID required")
	}

	d, err := did.Parse(id)
	if err != nil {
		log.Fatalf("Invalid DID: %s", err)
	} else if d.Method != "go" {
		log.Fatalf("Only 'go' DID methods can be registered.")
	} else if len(id) > MaxDIDLength {
		log.Fatalf("ID must be less than 32 characters")
	}

	// Parse key.
	acc, err := web3.ParsePrivateKey(privateKey)
	if err != nil {
		log.Fatalf("Cannot parse private key: %s", err)
	}
	publicKey := acc.Key().PublicKey

	// Build DID identifier.
	publicKeyID := *d
	publicKeyID.Fragment = "owner"

	// Build DID document.
	now := time.Now()
	doc := did.NewDocument()
	doc.ID = d.String()
	doc.Created = &now
	doc.Updated = &now
	doc.PublicKeys = []did.PublicKey{{
		ID:           publicKeyID.String(),
		Type:         "Secp256k1VerificationKey2018",
		Controller:   d.String(),
		PublicKeyHex: common.ToHex(crypto.FromECDSAPub(&publicKey)),
	}}
	doc.Authentications = []interface{}{publicKeyID.String()}

	// Pretty print document.
	data, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		log.Fatal(err)
	}

	// Upload to IPFS.
	hash, err := IPFSUpload(ctx, "did.json", data)
	if err != nil {
		log.Fatal(err)
	}

	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()

	myabi, err := abi.JSON(strings.NewReader(assets.DIDRegistryABI))
	if err != nil {
		log.Fatalf("Cannot initialize DIDRegistry ABI: %v", err)
	}

	var idBytes32 [32]byte
	copy(idBytes32[:], d.ID)

	tx, err := web3.CallTransactFunction(ctx, client, myabi, registryAddress, privateKey, "register", 0, idBytes32, hash)
	if err != nil {
		log.Fatalf("Cannot register DID identifier: %v", err)
	}

	ctx, _ = context.WithTimeout(ctx, 10*time.Second)
	receipt, err := web3.WaitForReceipt(ctx, client, tx.Hash)
	if err != nil {
		log.Fatalf("Cannot get the receipt: %v", err)
	}
	fmt.Println("Successfully registered DID:", d.String())
	fmt.Println("DID Document IPFS Hash:", hash)
	fmt.Println("Transaction address:", receipt.TxHash.Hex())
}

func DIDOwner(ctx context.Context, rpcURL, privateKey, id, registryAddress string) {
	if registryAddress == "" {
		log.Fatalf("Registry contract address required")
	}

	d, err := did.Parse(id)
	if err != nil {
		log.Fatalf("Invalid DID: %s", id)
	}

	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()

	myabi, err := abi.JSON(strings.NewReader(assets.DIDRegistryABI))
	if err != nil {
		log.Fatalf("Cannot initialize DIDRegistry ABI: %v", err)
	}

	var idBytes32 [32]byte
	copy(idBytes32[:], d.ID)

	result, err := web3.CallConstantFunction(ctx, client, myabi, registryAddress, "owner", idBytes32)
	if err != nil {
		log.Fatalf("Cannot call the contract: %v", err)
	}

	ctx, _ = context.WithTimeout(ctx, 10*time.Second)
	address := result.(common.Address)
	fmt.Println(address.Hex())
}

func DIDHash(ctx context.Context, rpcURL, privateKey, id, registryAddress string) {
	if registryAddress == "" {
		log.Fatalf("Registry contract address required")
	}

	d, err := did.Parse(id)
	if err != nil {
		log.Fatalf("Invalid DID: %s", err)
	}

	client, err := web3.Dial(rpcURL)
	if err != nil {
		log.Fatalf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()

	myabi, err := abi.JSON(strings.NewReader(assets.DIDRegistryABI))
	if err != nil {
		log.Fatalf("Cannot initialize DIDRegistry ABI: %v", err)
	}

	var idBytes32 [32]byte
	copy(idBytes32[:], d.ID)

	result, err := web3.CallConstantFunction(ctx, client, myabi, registryAddress, "hash", idBytes32)
	if err != nil {
		log.Fatalf("Cannot call the contract: %v", err)
	}

	ctx, _ = context.WithTimeout(ctx, 10*time.Second)
	hash := result.(string)
	fmt.Println(hash)
}

func ShowDID(ctx context.Context, rpcURL, privateKey, id, registryAddress string) {
	// Read current DID document for ID from IPFS.
	doc, err := readDIDDocument(ctx, rpcURL, registryAddress, id)
	if err != nil {
		log.Fatal(err)
	}

	// Pretty print document.
	data, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(data))
}

func SignClaim(ctx context.Context, rpcURL, privateKey, id, typ, issuerID, subjectID, subjectJSON string) {
	if id == "" {
		log.Fatalf("Credential ID required")
	} else if typ == "" {
		log.Fatalf("Credential type required")
	}
	if issuerID == "" {
		log.Fatalf("Credential issuer DID required")
	} else if _, err := did.Parse(issuerID); err != nil {
		log.Fatalf("Invalid credential issuer DID: %s", err)
	}
	if subjectID == "" {
		log.Fatalf("Credential subject DID required")
	} else if _, err := did.Parse(subjectID); err != nil {
		log.Fatalf("Invalid credential subject DID: %s", err)
	}

	// Parse key.
	acc, err := web3.ParsePrivateKey(privateKey)
	if err != nil {
		log.Fatalf("Cannot parse private key: %s", err)
	}

	// Parse subject object.
	subject := make(map[string]interface{})
	if subjectJSON != "" {
		if err := json.Unmarshal([]byte(subjectJSON), &subject); err != nil {
			log.Fatalf("Cannot parse subject JSON data: %s", err)
		}
	}
	subject["id"] = subjectID

	// Store current time to the second.
	now := time.Now().UTC().Truncate(1 * time.Second)

	// Build verifiable credential.
	cred := vc.NewVerifiableCredential()
	cred.ID = id
	cred.Type = append(cred.Type, typ)
	cred.Issuer = issuerID
	cred.IssuanceDate = &now
	cred.CredentialSubject = subject

	// Marshal data without proof.
	hw := sha3.NewLegacyKeccak256()
	if err := json.NewEncoder(hw).Encode(cred); err != nil {
		log.Fatalf("Cannot marshal credential to JSON: %s", err)
	}

	// Sign hash of credential document.
	var h common.Hash
	hw.Sum(h[:0])
	proofValue, err := crypto.Sign(h[:], acc.Key())
	if err != nil {
		log.Fatalf("Cannot sign credential: %s", err)
	}

	// Trim "V" off end of proof value.
	proofValue = proofValue[:len(proofValue)-1]

	// Add proof to credential.
	cred.Proof = &vc.Proof{
		Type:       "Secp256k1VerificationKey2018",
		Created:    &now,
		ProofValue: common.Bytes2Hex(proofValue),
	}

	// Pretty print credential.
	output, err := json.MarshalIndent(cred, "", "\t")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(output))
}

func VerifyClaim(ctx context.Context, rpcURL, privateKey, registryAddress, filename string) {
	// Decode file into VerifiableCredential.
	var cred vc.VerifiableCredential
	if buf, err := ioutil.ReadFile(filename); err != nil {
		log.Fatalf("Cannot read file: %s", err)
	} else if err := json.Unmarshal(buf, &cred); err != nil {
		log.Fatalf("Cannot decode credential: %s", err)
	}

	// Read issuer DID document.
	doc, err := readDIDDocument(ctx, rpcURL, registryAddress, cred.Issuer)
	if err != nil {
		log.Fatalf("Cannot read issuer DID document: %s", err)
	}

	// Encode credential to JSON without proof to generate hash.
	other := cred // shallow copy
	other.Proof = nil
	hw := sha3.NewLegacyKeccak256()
	if err := json.NewEncoder(hw).Encode(other); err != nil {
		log.Fatalf("Cannot hash claim: %s", err)
	}
	var h common.Hash
	hw.Sum(h[:0])

	// Attempt verification against each of issuer's public keys.
	// Only Secp256k1 is currently supported.
	var verified bool
	for _, pub := range doc.PublicKeys {
		if pub.Type != "Secp256k1VerificationKey2018" {
			continue
		}

		pubkey := common.Hex2Bytes(strings.TrimPrefix(pub.PublicKeyHex, "0x"))
		if crypto.VerifySignature(pubkey, h[:], common.Hex2Bytes(cred.Proof.ProofValue)) {
			verified = true
			break
		}
	}

	// Display error if no keys can verify the signature.
	if !verified {
		fmt.Println("Status: NOT VERIFIED")
		os.Exit(1)
	}

	// Extract subject & extract ID.
	subject := cred.CredentialSubject
	if subject == nil {
		subject = make(map[string]interface{})
	}
	subjectID := subject["id"]
	delete(subject, "id")

	// Sort subject keys.
	keys := make([]string, 0, len(subject))
	for k := range subject {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Display credential info on success.

	fmt.Printf("ID:      %s\n", cred.ID)
	fmt.Printf("Type:    %s\n", strings.Join(cred.Type, ", "))
	fmt.Println("Status:  VERIFIED")
	fmt.Println("")

	fmt.Printf("Subject:   %s\n", subjectID)
	fmt.Printf("Issuer:    %s\n", cred.Issuer)
	fmt.Printf("Issued On: %s\n", cred.IssuanceDate)
	fmt.Println("")

	if len(keys) != 0 {
		fmt.Println("CLAIMS:")
		for _, k := range keys {
			fmt.Printf("%s: %v\n", k, subject[k])
		}
		fmt.Println("")
	}
}

func marshalJSON(data interface{}) string {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fatalExit(fmt.Errorf("Cannot marshal json: %v", err))
	}
	return string(b)
}

func getAbi(contractFile string) *abi.ABI {
	abi, err := web3.ABIBuiltIn(contractFile)
	if err != nil {
		fatalExit(fmt.Errorf("Cannot get ABI from the bundled storage: %v", err))
	}
	if abi == nil {
		abi, err = web3.ABIOpenFile(contractFile)
		if err != nil {
			fatalExit(fmt.Errorf("Cannot get ABI: %v", err))
		}
	}
	return abi
}
func fatalExit(err error) {
	fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
	os.Exit(1)
}

// IPFSUpload uploads data to IPFS with a given filename.
func IPFSUpload(ctx context.Context, name string, data []byte) (string, error) {
	// Build multi-part request body.
	var body bytes.Buffer
	mpw := multipart.NewWriter(&body)
	if part, err := mpw.CreateFormFile("file", name); err != nil {
		return "", err
	} else if _, err := part.Write(data); err != nil {
		return "", err
	} else if err := mpw.Close(); err != nil {
		return "", err
	}

	// Execute POST against Infura API.
	resp, err := http.Post("https://ipfs.infura.io:5001/api/v0/add?pin=true", mpw.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Unmarshal into data structure to extract hash.
	var jsonResp struct {
		Name string
		Hash string
		Size string
	}
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		return "", err
	}
	return jsonResp.Hash, nil
}

func readDIDDocument(ctx context.Context, rpcURL, registryAddress, id string) (*did.Document, error) {
	if registryAddress == "" {
		return nil, fmt.Errorf("Registry contract address required")
	}

	d, err := did.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("Invalid DID: %s", id)
	}

	client, err := web3.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to %q: %v", rpcURL, err)
	}
	defer client.Close()

	myabi, err := abi.JSON(strings.NewReader(assets.DIDRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("Cannot initialize DIDRegistry ABI: %v", err)
	}

	var idBytes32 [32]byte
	copy(idBytes32[:], d.ID)

	result, err := web3.CallConstantFunction(ctx, client, myabi, registryAddress, "hash", idBytes32)
	if err != nil {
		return nil, fmt.Errorf("Cannot call the contract: %v", err)
	}

	ctx, _ = context.WithTimeout(ctx, 10*time.Second)

	hash := result.(string)
	resp, err := http.Get(fmt.Sprintf("https://ipfs.infura.io:5001/api/v0/cat?arg=%s", hash))
	if err != nil {
		return nil, fmt.Errorf("Unable to fetch DID document from IPFS: %s", err)
	}
	defer resp.Body.Close()

	var doc did.Document
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("Unable to decode DID document: %s", err)
	}
	return &doc, nil
}
