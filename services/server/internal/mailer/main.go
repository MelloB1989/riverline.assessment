package mailer

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"

	"riverline_server/constants"

	"github.com/MelloB1989/karma/mails"
	m "github.com/MelloB1989/karma/models"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
)

type Template struct {
	ToEmail string
	Subject string
	Text    string
	HTML    string
}

type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

func (t *Template) Send() error {
	from := constants.AppCfg.Get().MailerAddress
	if from == "" || t.ToEmail == "" {
		return nil
	}
	km := mails.NewKarmaMail(from, mails.AWS_SES)

	// Send email
	if err := km.SendSingleMail(m.SingleEmailRequest{
		To: t.ToEmail,
		Email: m.Email{
			Subject: t.Subject,
			Body: m.EmailBody{
				Text: t.Text,
				HTML: t.HTML,
			},
		},
	}); err != nil {
		return err
	}

	return nil
}

func (t *Template) SendWithAttachment(attachment Attachment) error {
	from := constants.AppCfg.Get().MailerAddress
	if from == "" || t.ToEmail == "" {
		return nil
	}
	if len(attachment.Data) == 0 || strings.TrimSpace(attachment.Filename) == "" {
		return t.Send()
	}
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return err
	}
	if region := strings.TrimSpace(constants.AppCfg.Get().AwsSesRegion); region != "" {
		cfg.Region = region
	}
	raw, err := t.rawMIME(from, attachment)
	if err != nil {
		return err
	}
	client := ses.NewFromConfig(cfg)
	_, err = client.SendRawEmail(context.TODO(), &ses.SendRawEmailInput{
		Destinations: []string{t.ToEmail},
		RawMessage:   &types.RawMessage{Data: raw},
		Source:       aws.String(from),
	})
	return err
}

func (t *Template) rawMIME(from string, attachment Attachment) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", t.ToEmail)
	fmt.Fprintf(&buf, "Subject: %s\r\n", t.Subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", writer.Boundary())

	altHeader := textproto.MIMEHeader{}
	altHeader.Set("Content-Type", `multipart/alternative; boundary="riverline-alt"`)
	alt, err := writer.CreatePart(altHeader)
	if err != nil {
		return nil, err
	}
	writeAlternativePart(alt, t.Text, t.HTML)

	partHeader := textproto.MIMEHeader{}
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	partHeader.Set("Content-Type", fmt.Sprintf(`%s; name="%s"`, contentType, attachment.Filename))
	partHeader.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, attachment.Filename))
	partHeader.Set("Content-Transfer-Encoding", "base64")
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(attachment.Data)
	for len(encoded) > 76 {
		fmt.Fprintf(part, "%s\r\n", encoded[:76])
		encoded = encoded[76:]
	}
	if encoded != "" {
		fmt.Fprintf(part, "%s\r\n", encoded)
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeAlternativePart(w io.Writer, textBody string, htmlBody string) {
	fmt.Fprint(w, "--riverline-alt\r\n")
	fmt.Fprint(w, "Content-Type: text/plain; charset=utf-8\r\n\r\n")
	fmt.Fprint(w, strings.TrimSpace(textBody))
	fmt.Fprint(w, "\r\n--riverline-alt\r\n")
	fmt.Fprint(w, "Content-Type: text/html; charset=utf-8\r\n\r\n")
	fmt.Fprint(w, strings.TrimSpace(htmlBody))
	fmt.Fprint(w, "\r\n--riverline-alt--\r\n")
}
