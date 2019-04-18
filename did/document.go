package did

import (
	"time"
)

// ContextV1 is the required context for all DID documents.
const ContextV1 = "https://w3id.org/did/v1"

// Document represents a DID document.
type Document struct {
	// MUST be set to ContextV1.
	Context string `json:"@context"`

	// The identifier that the DID Document is about, i.e. the DID.
	ID string `json:"id"`

	// Public keys are used for digital signatures, encryption and other
	// cryptographic operations, which in turn are the basis for purposes such
	// as authentication, or establishing secure communication with service
	// endpoints. In addition, public keys may play a role in authorization
	// mechanisms of DID CRUD operations
	PublicKeys []PublicKey `json:"publicKey"`

	// Specifies zero or more embedded or referenced public keys by which a
	// DID subject can cryptographically prove that they are associated with a DID.
	//
	// Each element MUST be a PublicKey (embedded) or string (referenced).
	Authentications []interface{} `json:"authentication"`

	// Represent any type of service the subject wishes to advertise, including
	// decentralized identity management services for further discovery,
	// authentication, authorization, or interaction.
	Services []Service

	// Timestamp when document was first created, normalized to UTC. Optional.
	Created *time.Time `json:"created"`

	// Timestamp when document was last updated, normalized to UTC. Optional.
	Updated *time.Time `json:"updated"`

	// Cryptographic proof of the integrity of the DID Document.
	// This proof is NOT proof of the binding between a DID and a DID Document.
	Proof *Proof `json:"proof"`
}

// NewDocument returns a new Document with the appropriate context.
func NewDocument() *Document {
	return &Document{Context: ContextV1}
}

// PublicKey represents a specification of public key on the document.
type PublicKey struct {
	// Unique identifier of the key within the document.
	ID string `json:"id"`

	// Type of encryption, as specified in Linked Data Cryptographic Suite Registry.
	// https://w3c-ccg.github.io/ld-cryptosuite-registry/
	Type string `json:"type"`

	// DID identifying the controller of the corresponding private key.
	Controller string `json:"controller"`

	// Only one of these can be specified based on type.
	PublicKeyPEM       string `json:"publicKeyPem"`
	PublicKeyJWK       string `json:"publicKeyJwk"`
	PublicKeyHex       string `json:"publicKeyHex"`
	PublicKeyBase64    string `json:"publicKeyBase64"`
	PublicKeyBase58    string `json:"publicKeyBase58"`
	PublicKeyMultibase string `json:"publicKeyMultibase"`
}

// Service represents a service endpoint specification.
type Service struct {
	// Unique identifier of the service within the document.
	ID              string `json:"id"`
	Type            string `json:"type"`
	ServiceEndpoint string `json:"serviceEndpoint"`
}

// Proof represents a JSON-LD proof of the integrity of a DID document.
type Proof struct {
	Type           string `json:"type"`
	Creator        string `json:"creator"`
	Created        string `json:"created"`
	Domain         string `json:"domain"`
	Nonce          string `json:"nonce"`
	SignatureValue string `json:"signatureValue"`
}
