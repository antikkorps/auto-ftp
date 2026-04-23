package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

const (
	ftpPort   = 2121
	pasvStart = 2122
	pasvEnd   = 2130
)

type driver struct {
	rootFs   afero.Fs
	logger   *slog.Logger
	settings *ftpserver.Settings
}

func buildSettings() *ftpserver.Settings {
	return &ftpserver.Settings{
		ListenAddr:               fmt.Sprintf("0.0.0.0:%d", ftpPort),
		Banner:                   "EasyVIEW FTP",
		PassiveTransferPortRange: ftpserver.PortRange{Start: pasvStart, End: pasvEnd},
		IdleTimeout:              900,
	}
}

func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	if d.settings != nil {
		return d.settings, nil
	}
	return buildSettings(), nil
}

func checkCreds(user, pass string) error {
	if user != ftpUser || pass != ftpPass {
		return errors.New("invalid credentials")
	}
	return nil
}

func (d *driver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	d.logger.Info("client connected", "remote", cc.RemoteAddr().String(), "id", cc.ID())
	return "EasyVIEW FTP", nil
}

func (d *driver) ClientDisconnected(cc ftpserver.ClientContext) {
	d.logger.Info("client disconnected", "remote", cc.RemoteAddr().String(), "id", cc.ID())
}

func (d *driver) GetTLSConfig() (*tls.Config, error) {
	return nil, errors.New("TLS not enabled")
}

func (d *driver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if err := checkCreds(user, pass); err != nil {
		d.logger.Warn("auth failed", "user", user, "remote", cc.RemoteAddr().String())
		return nil, err
	}
	d.logger.Info("auth ok", "user", user, "remote", cc.RemoteAddr().String())
	return d.rootFs, nil
}

func shortBindError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "address already in use"),
		strings.Contains(lower, "only one usage of each socket"),
		strings.Contains(lower, "bind: address in use"):
		return fmt.Sprintf("PORT %d DÉJÀ UTILISÉ", ftpPort)
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "access is denied"):
		return "PERMISSION REFUSÉE"
	default:
		return "DÉMARRAGE IMPOSSIBLE"
	}
}
