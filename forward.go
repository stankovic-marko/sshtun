package sshtun

import (
	"context"
	"fmt"
	"io"
	"net"

	"golang.org/x/sync/errgroup"
)

// TunneledConnState represents the state of the final connections made through the tunnel.
type TunneledConnState struct {
	// From is the address initating the connection.
	From string
	// Info holds a message with info on the state of the connection (useful for debug purposes).
	Info string
	// Error holds an error on the connection or nil if the connection is successful.
	Error error
	// Ready indicates if the connection is established.
	Ready bool
	// Closed indicates if the connection is closed.
	Closed bool
}

func (s *TunneledConnState) String() string {
	out := fmt.Sprintf("[%s] ", s.From)
	if s.Info != "" {
		out += s.Info
	}
	if s.Error != nil {
		out += fmt.Sprintf("Error: %v", s.Error)
	}
	return out
}

func (tun *SSHTun) forward(fromConn net.Conn) {
	from := fromConn.RemoteAddr().String()

	if tun.forwardType == Local {
		tun.tunneledState(&TunneledConnState{
			From: from,
			Info: fmt.Sprintf("accepted %s connection", tun.local.Type()),
		})
	} else if tun.forwardType == Remote {
    tun.tunneledState(&TunneledConnState{
      From: from,
      Info: fmt.Sprintf("accepted %s connection", tun.remote.Type()),
    })
  }

	var toConn net.Conn
	var err error
	
	if tun.forwardType == Local {
		toConn, err = tun.sshClient.Dial(tun.remote.Type(), tun.remote.String())
		if err != nil {
			tun.tunneledState(&TunneledConnState{
				From:  from,
				Error: fmt.Errorf("remote dial %s to %s failed: %w", tun.remote.Type(), tun.remote.String(), err),
			})

			fromConn.Close()
			return
		}
	}
	if tun.forwardType == Remote {
		toConn, err = net.Dial(tun.local.Type(), tun.local.String())
		if err != nil {
			tun.tunneledState(&TunneledConnState{
				From:  from,
				Error: fmt.Errorf("local dial %s to %s failed: %w", tun.local.Type(), tun.local.String(), err),
			})

			fromConn.Close()
			return
		}
	}

	connStr := fmt.Sprintf("%s -(%s)> %s -(ssh)> %s -(%s)> %s", from, tun.local.Type(), tun.local.String(),
		tun.server.String(), tun.remote.Type(), tun.remote.String())

	tun.tunneledState(&TunneledConnState{
		From:   from,
		Info:   fmt.Sprintf("connection established: %s", connStr),
		Ready:  true,
		Closed: false,
	})

	connCtx, connCancel := context.WithCancel(tun.ctx)
	errGroup := &errgroup.Group{}

	errGroup.Go(func() error {
		defer connCancel()
		_, err = io.Copy(toConn, fromConn)
		if err != nil {
			if tun.forwardType == Local {
				return fmt.Errorf("failed copying bytes from remote to local: %w", err)
			} else if tun.forwardType == Remote {
				return fmt.Errorf("failed copying bytes from local to remote: %w", err)
			}
		}
		return toConn.Close()
	})

	errGroup.Go(func() error {
		defer connCancel()
		_, err = io.Copy(fromConn, toConn)
		if err != nil {
			if tun.forwardType == Local {
				return fmt.Errorf("failed copying bytes from local to remote: %w", err)
			} else if tun.forwardType == Remote {
				return fmt.Errorf("failed copying bytes from remote to local: %w", err)
			}
		}
		return fromConn.Close()
	})

	err = errGroup.Wait()

	<-connCtx.Done()

	select {
	case <-tun.ctx.Done():
	default:
		if err != nil {
			tun.tunneledState(&TunneledConnState{
				From:  from,
				Error: err,
				Closed: true,
			})
		}
	}

	tun.tunneledState(&TunneledConnState{
		From:   from,
		Info:   fmt.Sprintf("connection closed: %s", connStr),
		Ready:  false,
		Closed: true,
	})
}

func (tun *SSHTun) tunneledState(state *TunneledConnState) {
	if tun.tunneledConnState != nil {
		tun.tunneledConnState(tun, state)
	}
}
