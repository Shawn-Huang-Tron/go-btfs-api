package shell

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/tron-us/go-common/v2/json"
	"strconv"
	"time"

	utils "github.com/TRON-US/go-btfs-api/utils"

	ic "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/tron-us/go-btfs-common/crypto"
	escrowpb "github.com/tron-us/go-btfs-common/protos/escrow"
	guardpb "github.com/tron-us/go-btfs-common/protos/guard"
	ledgerpb "github.com/tron-us/go-btfs-common/protos/ledger"
	"github.com/tron-us/go-common/v2/log"

	"go.uber.org/zap"
)

type StorageUploadOpts = func(*RequestBuilder) error

type storageUploadResponse struct {
	ID string
}

type Shard struct {
	ContractId string
	Price      int64
	Host       string
	Status     string
}

type Storage struct {
	Status   string
	Message  string
	FileHash string
	Shards   map[string]Shard
}

type ContractItem struct {
	Key      string `json:"key"`
	Contract string `json:"contract"`
}

type Contracts struct {
	Contracts []ContractItem `json:contracts`
}

type UnsignedData struct {
	Unsigned string
	Opcode   string
	Price    int64
}

type StorageOpts = func(*RequestBuilder) error

func UploadMode(mode string) StorageOpts {
	return func(rb *RequestBuilder) error {
		rb.Option("m", mode)
		return nil
	}
}

func Hosts(hosts string) StorageOpts {
	return func(rb *RequestBuilder) error {
		rb.Option("s", hosts)
		return nil
	}
}


func (d UnsignedData) SignData(privateKey string) ([]byte, error) {
	privKey, _ := crypto.ToPrivKey(privateKey)
	signedData, err := privKey.Sign([]byte(d.Unsigned))
	if err != nil {
		return nil, err
	}
	return signedData, nil
}

func (d UnsignedData) SignBalanceData(privateKey string) (*ledgerpb.SignedPublicKey, error) {
	privKey, _ := crypto.ToPrivKey(privateKey)
	pubKeyRaw, err := privKey.GetPublic().Raw()
	if err != nil {
		return &ledgerpb.SignedPublicKey{}, err
	}
	lgPubKey := &ledgerpb.PublicKey{
		Key: pubKeyRaw,
	}
	sig, err := crypto.Sign(privKey, lgPubKey)
	if err != nil {
		return &ledgerpb.SignedPublicKey{}, err
	}
	lgSignedPubKey := &ledgerpb.SignedPublicKey{
		Key:       lgPubKey,
		Signature: sig,
	}
	return lgSignedPubKey, nil
}

const (
	Text = iota + 1
	Base64
)

func stringToBytes(str string, encoding int) ([]byte, error) {
	switch encoding {
	case Text:
		return []byte(str), nil
	case Base64:
		by, err := base64.StdEncoding.DecodeString(str)
		if err != nil {
			return nil, err
		}
		return by, nil
	default:
		return nil, fmt.Errorf(`unexpected encoding [%d], expected 1(Text) or 2(Base64)`, encoding)
	}
}
func bytesToString(data []byte, encoding int) (string, error) {
	switch encoding {
	case Text:
		return string(data), nil
	case Base64:
		return base64.StdEncoding.EncodeToString(data), nil
	default:
		return "", fmt.Errorf(`unexpected parameter [%d] is given, either "text" or "base64" should be used`, encoding)
	}
}
func (c Contracts) SignContracts(privateKey string, sessionStatus string) (*Contracts, error) {
	// Perform signing using private key
	privKey, err := crypto.ToPrivKey(privateKey)
	if err != nil {
		log.Error("%s", zap.Error(err))
	}
	for idx, element := range c.Contracts {
		by, err := stringToBytes(element.Contract, Base64)
		if err != nil {
			return nil, err
		}
		var signedContract []byte
		if sessionStatus == "initSignReadyEscrow" {
			escrowContract := &escrowpb.EscrowContract{}

			err = proto.Unmarshal(by, escrowContract)
			if err != nil {
				return nil, err
			}
			signedContract, err = crypto.Sign(privKey, escrowContract)
			if err != nil {
				return nil, err
			}

		} else {
			guardContract := &guardpb.ContractMeta{}
			//var guardContract proto.Message
			err := proto.Unmarshal(by, guardContract)
			if err != nil {
				return nil, err
			}
			signedContract, err = crypto.Sign(privKey, guardContract)
			if err != nil {
				return nil, err
			}
		}
		// This overwrites
		str, err := bytesToString(signedContract, Base64)
		if err != nil {
			return nil, err
		}
		c.Contracts[idx].Contract = str
		if err != nil {
			return nil, err
		}
	}

	return &c, nil
}

// Set storage upload time.
func StorageLength(length int) StorageUploadOpts {
	return func(rb *RequestBuilder) error {
		rb.Option("storage-length", length)
		return nil
	}
}

func (s *Shell) GetUts() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

func getSessionSignature(hash string, peerId string) (string, time.Time) {
	//offline session signature
	now := time.Now()
	sessionSignature := fmt.Sprintf("%s:%s:%s", utils.PeerId, hash, "time.Now().String()")
	return sessionSignature, now
}

// Storage upload api.
func (s *Shell) StorageUpload(hash string, options ...StorageUploadOpts) (string, error) {
	var out storageUploadResponse
	rb := s.Request("storage/upload", hash)
	for _, option := range options {
		_ = option(rb)
	}
	return out.ID, rb.Exec(context.Background(), &out)
}

// Storage upload api.
func (s *Shell) StorageUploadOffSign(hash string, uts string, options ...StorageUploadOpts) (string, error) {
	var out storageUploadResponse
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)
	rb := s.Request("storage/upload/offline", hash, utils.PeerId, uts, offlinePeerSessionSignature)
	for _, option := range options {
		_ = option(rb)
	}
	return out.ID, rb.Exec(context.Background(), &out)
}

// Storage upload status api.
func (s *Shell) StorageUploadStatus(id string) (Storage, error) {
	var out Storage
	rb := s.Request("storage/upload/status", id)
	return out, rb.Exec(context.Background(), &out)
}

// Storage upload get offline contract batch api.
func (s *Shell) StorageUploadGetContractBatch(sid string, hash string, uts string, sessionStatus string) (Contracts, error) {
	//var out Contracts
	var out Contracts
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)
	rb := s.Request("storage/upload/getcontractbatch", sid, utils.PeerId, uts, offlinePeerSessionSignature, sessionStatus)
	return out, rb.Exec(context.Background(), &out)
}

// Storage upload get offline unsigned data api.
func (s *Shell) StorageUploadGetUnsignedData(sid string, hash string, uts string, sessionStatus string) (UnsignedData, error) {
	var out UnsignedData
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)
	rb := s.Request("storage/upload/getunsigned", sid, utils.PeerId, uts, offlinePeerSessionSignature, sessionStatus)
	return out, rb.Exec(context.Background(), &out)
}

// Storage upload sign offline contract batch api.
func (s *Shell) StorageUploadSignBatch(sid string, hash string, unsignedBatchContracts Contracts, uts string, sessionStatus string) ([]byte, error) {
	var out []byte
	var signedBatchContracts *Contracts
	var errSign error
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)

	if utils.PrivateKey != "" {
		signedBatchContracts, errSign = unsignedBatchContracts.SignContracts(utils.PrivateKey, sessionStatus)
		if errSign != nil {
			log.Error("%s", zap.Error(errSign))
		}
		bytesSignBatch, err := json.Marshal(signedBatchContracts.Contracts)
		if err != nil {
			return nil, err
		}

		rb := s.Request("storage/upload/signcontractbatch", sid, utils.PeerId, uts, offlinePeerSessionSignature,
			sessionStatus, string(bytesSignBatch))
		return out, rb.Exec(context.Background(), &out)
	}
	return nil, errors.New("private key not available in configuration file or environment variable")
}

// Storage upload sign offline data api.
func (s *Shell) StorageUploadSign(id string, hash string, unsignedData UnsignedData, uts string, sessionStatus string) ([]byte, error) {
	var out []byte
	var rb *RequestBuilder
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)
	if utils.PrivateKey != "" {
		signedBytes, err := unsignedData.SignData(utils.PrivateKey)
		if err != nil {
			log.Error("%s", zap.Error(err))
		}
		rb = s.Request("storage/upload/sign", id, utils.PeerId, uts, offlinePeerSessionSignature, string(signedBytes), sessionStatus)
		return out, rb.Exec(context.Background(), &out)
	}
	return nil, errors.New("private key not available in configuration file or environment variable")
}

const DEBUG = true
func (s *Shell) StorageUploadSignBalance(id string, hash string, unsignedData UnsignedData, uts string, sessionStatus string) ([]byte, error) {
	var out []byte
	var rb *RequestBuilder
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)
	if utils.PrivateKey != "" {
		ledgerSignedPublicKey, err := unsignedData.SignBalanceData(utils.PrivateKey)
		if err != nil {
			log.Error("%s", zap.Error(err))
		}
		signedBytes, err := proto.Marshal(ledgerSignedPublicKey)    // TODO: check if ic.Marshall is necessary!
		if err != nil {
			return nil, err
		}
		str, err := bytesToString(signedBytes, Base64)
		if err != nil {
			return nil, err
		}
		if DEBUG {
			signedBytes, err := stringToBytes(str, Base64)
			if err != nil {
				return nil, err
			}

			var lgSignedPubKey ledgerpb.SignedPublicKey
			err = proto.Unmarshal(signedBytes, &lgSignedPubKey)
			if err != nil {
				return nil, err
			}

			fmt.Println(lgSignedPubKey)
		}
		rb = s.Request("storage/upload/sign", id, utils.PeerId, uts, offlinePeerSessionSignature, str, sessionStatus)
		return out, rb.Exec(context.Background(), &out)
	}
	return nil, errors.New("private key not available in configuration file or environment variable")
}

func (s *Shell) StorageUploadSignPayChannel(id, hash string, unsignedData UnsignedData, uts string, sessionStatus string, totalPrice int64) ([]byte, error) {
	var out []byte
	var rb *RequestBuilder
	offlinePeerSessionSignature, now := getSessionSignature(hash, utils.PeerId)
	if utils.PrivateKey != "" {
		unsignedBytes, err := stringToBytes(unsignedData.Unsigned, Base64)
		if err != nil {
			return nil, err
		}
		escrowPubKey, err := ic.UnmarshalPublicKey(unsignedBytes)
		if err != nil {
			return nil, err
		}
		buyerPubKey, err := crypto.ToPubKey(utils.PublicKey)
		if err != nil {
			return nil, err
		}
		fromAddr, err := ic.RawFull(buyerPubKey)
		if err != nil {
			return nil, err
		}
		toAddr, err := ic.RawFull(escrowPubKey)
		if err != nil {
			return nil, err
		}
		chanCommit := &ledgerpb.ChannelCommit{
			Payer:     &ledgerpb.PublicKey{Key: fromAddr},
			Recipient: &ledgerpb.PublicKey{Key: toAddr},
			Amount: totalPrice,
			PayerId: now.UnixNano(),
		}
		buyerPrivKey, err := crypto.ToPrivKey(utils.PrivateKey)
		if err != nil {
			return nil, err
		}
		buyerChanSig, err := crypto.Sign(buyerPrivKey, chanCommit)
		if err != nil {
			return nil, err
		}
		signedChanCommit := &ledgerpb.SignedChannelCommit{
			Channel:   chanCommit,
			Signature: buyerChanSig,
		}
		signedChanCommitBytes, err := proto.Marshal(signedChanCommit)
		if err != nil {
			return nil, err
		}
		signedChanCommitBytesStr, err := bytesToString(signedChanCommitBytes, Base64)
		if err != nil {
			return nil, err
		}
		rb = s.Request("storage/upload/sign", id, utils.PeerId, uts, offlinePeerSessionSignature, signedChanCommitBytesStr, sessionStatus)
		return out, rb.Exec(context.Background(), &out)
	}
	return nil, errors.New("private key not available in configuration file or environment variable")
}

func (s *Shell) StorageUploadSignPayRequest(id, hash string, unsignedData UnsignedData,
	uts string, sessionStatus string) ([]byte, error) {
	var out []byte
	var rb *RequestBuilder
	offlinePeerSessionSignature, _ := getSessionSignature(hash, utils.PeerId)
	if utils.PrivateKey != "" {
		result := new(escrowpb.SignedSubmitContractResult)
		err := proto.Unmarshal([]byte(unsignedData.Unsigned), result)
		if err != nil {
			return nil, err
		}

		chanState := result.Result.BuyerChannelState
		privKey, _ := crypto.ToPrivKey(utils.PrivateKey)
		sig, err := crypto.Sign(privKey, chanState)
		if err != nil {
			return nil, err
		}
		chanState.FromSignature = sig
		payerPubKey, _ := crypto.ToPrivKey(utils.PublicKey)
		payerAddr, err := payerPubKey.Raw()
		if err != nil {
			return nil, err
		}
		payinReq := &escrowpb.PayinRequest{
			PayinId:           result.Result.PayinId,
			BuyerAddress:      payerAddr,
			BuyerChannelState: chanState,
		}
		payinSig, err := crypto.Sign(privKey, payinReq)
		if err != nil {
			return nil, err
		}
		signedPayinReq := &escrowpb.SignedPayinRequest{
			Request:        payinReq,
			BuyerSignature: payinSig,
		}

		signedPayinReqBytes, err := proto.Marshal(signedPayinReq)
		if err != nil {
			return nil, err
		}

		rb = s.Request("storage/upload/sign", id, utils.PeerId, uts, offlinePeerSessionSignature, string(signedPayinReqBytes), sessionStatus)
		return out, rb.Exec(context.Background(), &out)
	}
	return nil, errors.New("private key not available in configuration file or environment variable")
}
