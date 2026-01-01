package obfuscated2

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

type serverHandshakeFrame struct {
	handshakeFrame
}

func (s *serverHandshakeFrame) decryptor() (cipher.Stream, error) {
	invertedHandshake := s.invert()

	return makeAesCtr(invertedHandshake.key(), invertedHandshake.iv())
}

func (s *serverHandshakeFrame) encryptor() (cipher.Stream, error) {
	return makeAesCtr(s.key(), s.iv())
}

func ServerHandshake(writer io.Writer) (cipher.Stream, cipher.Stream, error) {
	handshake, err := generateServerHanshakeFrame()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate server handshake: %w", err)
	}

	copyHandshake := handshake
	encryptor, err := handshake.encryptor()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create encryptor: %w", err)
	}

	decryptor, err := handshake.decryptor()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create decryptor: %w", err)
	}

	encryptor.XORKeyStream(handshake.data[:], handshake.data[:])
	copy(handshake.key(), copyHandshake.key())
	copy(handshake.iv(), copyHandshake.iv())

	if _, err := writer.Write(handshake.data[:]); err != nil {
		return nil, nil, fmt.Errorf("cannot send a handshake frame to telegram: %w", err)
	}

	return encryptor, decryptor, nil
}

func generateServerHanshakeFrame() (serverHandshakeFrame, error) {
	frame := serverHandshakeFrame{}

	for {
		if _, err := rand.Read(frame.data[:]); err != nil {
			return serverHandshakeFrame{}, fmt.Errorf("cannot generate random data: %w", err)
		}

		if frame.data[0] == 0xef { //nolint: gomnd // taken from tg sources
			continue
		}

		switch binary.LittleEndian.Uint32(frame.data[:4]) {
		case 0x44414548, 0x54534f50, 0x20544547, 0x4954504f, 0xeeeeeeee: //nolint: gomnd // taken from tg sources
			continue
		}

		if frame.data[4]|frame.data[5]|frame.data[6]|frame.data[7] == 0 {
			continue
		}

		copy(frame.connectionType(), handshakeConnectionType)

		return frame, nil
	}
}
