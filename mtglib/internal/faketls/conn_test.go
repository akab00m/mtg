package faketls_test

import (
	"bytes"
	"errors"
	"io"
	"math/rand"
	"testing"

	"github.com/9seconds/mtg/v2/internal/testlib"
	"github.com/9seconds/mtg/v2/mtglib/internal/faketls"
	"github.com/9seconds/mtg/v2/mtglib/internal/faketls/record"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type ConnMock struct {
	testlib.EssentialsConnMock

	readBuffer  bytes.Buffer
	writeBuffer bytes.Buffer
}

func (m *ConnMock) Read(p []byte) (int, error) {
	m.Called(p)

	return m.readBuffer.Read(p) //nolint: wrapcheck
}

func (m *ConnMock) Write(p []byte) (int, error) {
	m.Called(p)

	return m.writeBuffer.Write(p) //nolint: wrapcheck
}

type ConnTestSuite struct {
	suite.Suite

	connMock *ConnMock
	c        *faketls.Conn
}

func (suite *ConnTestSuite) SetupTest() {
	suite.connMock = &ConnMock{}
	suite.c = &faketls.Conn{
		Conn: suite.connMock,
	}
}

func (suite *ConnTestSuite) TearDownTest() {
	suite.connMock.AssertExpectations(suite.T())
}

func (suite *ConnTestSuite) TestRead() {
	suite.connMock.On("Read", mock.Anything).Return(0, nil)

	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	rec.Type = record.TypeChangeCipherSpec
	rec.Version = record.Version12

	rec.Payload.WriteByte(0x01)
	rec.Dump(&suite.connMock.readBuffer) //nolint: errcheck
	rec.Reset()

	rec.Type = record.TypeApplicationData
	rec.Version = record.Version12

	rec.Payload.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	rec.Dump(&suite.connMock.readBuffer) //nolint: errcheck

	resultBuffer := &bytes.Buffer{}
	buf := make([]byte, 2)

	for {
		n, err := suite.c.Read(buf)
		if errors.Is(err, io.EOF) {
			break
		}

		resultBuffer.Write(buf[:n])
	}

	suite.Equal([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, resultBuffer.Bytes())
}

func (suite *ConnTestSuite) TestReadUnexpected() {
	suite.connMock.On("Read", mock.Anything).Return(0, nil)

	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	rec.Type = record.TypeChangeCipherSpec
	rec.Version = record.Version12

	rec.Payload.WriteByte(0x01)
	rec.Dump(&suite.connMock.readBuffer) //nolint: errcheck
	rec.Reset()

	rec.Type = record.TypeHandshake
	rec.Version = record.Version12

	rec.Payload.Write([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	rec.Dump(&suite.connMock.readBuffer) //nolint: errcheck

	buf := make([]byte, 2)

	for {
		_, err := suite.c.Read(buf)

		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			suite.FailNow("unexpected to finish")
		default:
			return
		}
	}
}

func (suite *ConnTestSuite) TestWrite() {
	suite.connMock.On("Write", mock.Anything).Return(0, nil)

	dataToRec := make([]byte, record.TLSMaxRecordSize*2)
	rand.Read(dataToRec)

	n, err := suite.c.Write(dataToRec)
	suite.NoError(err)
	suite.Equal(len(dataToRec), n)

	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	buf := &bytes.Buffer{}

	for {
		if err := rec.Read(&suite.connMock.writeBuffer); err != nil {
			break
		}

		suite.Equal(record.TypeApplicationData, rec.Type)
		suite.Equal(record.Version12, rec.Version)
		rec.Payload.WriteTo(buf) //nolint: errcheck
	}

	suite.Equal(dataToRec, buf.Bytes())
}

// TestWriteChromeLikeRecordSizes проверяет, что Chrome-like распределение
// создаёт полные 16384-байтные records для больших данных.
func (suite *ConnTestSuite) TestWriteChromeLikeRecordSizes() {
	suite.connMock.On("Write", mock.Anything).Return(0, nil)

	// 16384*3 + 100 байт — ожидаем 3 полных record + 1 с остатком
	dataSize := record.TLSMaxWriteRecordSize*3 + 100
	data := make([]byte, dataSize)
	rand.Read(data)

	n, err := suite.c.Write(data)
	suite.NoError(err)
	suite.Equal(dataSize, n)

	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	var recordSizes []int

	for {
		if err := rec.Read(&suite.connMock.writeBuffer); err != nil {
			break
		}

		suite.Equal(record.TypeApplicationData, rec.Type)
		recordSizes = append(recordSizes, rec.Payload.Len())
	}

	// Ожидаем 4 records: 3 × 16384 + 1 × 100
	suite.Len(recordSizes, 4)
	suite.Equal(record.TLSMaxWriteRecordSize, recordSizes[0])
	suite.Equal(record.TLSMaxWriteRecordSize, recordSizes[1])
	suite.Equal(record.TLSMaxWriteRecordSize, recordSizes[2])
	suite.Equal(100, recordSizes[3])
}

// TestWriteWithCCSPadding проверяет, что CCS padding не повреждает данные.
// CCS records должны игнорироваться при reconstruction данных.
// Данные > 16384 байт (несколько records), т.к. CCS inject требует totalRecords > 1.
func (suite *ConnTestSuite) TestWriteWithCCSPadding() {
	suite.c.EnableCCSPadding = true
	suite.connMock.On("Write", mock.Anything).Return(0, nil)

	// Многократная запись — CCS инъекция вероятностная (~15%),
	// при 100 итерациях хотя бы одна должна содержать CCS
	rec := record.AcquireRecord()
	defer record.ReleaseRecord(rec)

	ccsCount := 0

	// Размер данных > 16384 — нужно минимум 2 records для CCS inject
	dataSize := record.TLSMaxWriteRecordSize*2 + 100

	for i := 0; i < 100; i++ {
		suite.connMock.writeBuffer.Reset()

		data := make([]byte, dataSize)
		rand.Read(data)

		n, err := suite.c.Write(data)
		suite.NoError(err)
		suite.Equal(dataSize, n)

		// Reconstruction: собираем ApplicationData, считаем CCS
		reconstructed := &bytes.Buffer{}

		for {
			if err := rec.Read(&suite.connMock.writeBuffer); err != nil {
				break
			}

			switch rec.Type { //nolint: exhaustive
			case record.TypeApplicationData:
				rec.Payload.WriteTo(reconstructed) //nolint: errcheck
			case record.TypeChangeCipherSpec:
				ccsCount++
				// CCS payload должен быть [0x01]
				suite.Equal([]byte{0x01}, rec.Payload.Bytes())
			default:
				suite.FailNow("unexpected record type: %v", rec.Type)
			}
		}

		// Данные не повреждены
		suite.Equal(data, reconstructed.Bytes())
	}

	// При 15% вероятности, 100 итераций — ожидаем хотя бы 1 CCS
	suite.Greater(ccsCount, 0, "CCS padding should inject at least 1 CCS record in 100 iterations")
}

func TestConn(t *testing.T) {
	t.Parallel()
	suite.Run(t, &ConnTestSuite{})
}
