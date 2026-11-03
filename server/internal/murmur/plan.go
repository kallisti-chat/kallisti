package murmur

import (
	"fmt"
	"strings"

	"kallisti.chat/server/internal/mumbled"
	"kallisti.chat/server/internal/names"
)

// Rename: a user whose name an earlier server's user had.
type Rename struct{ Server, From, To string }

// Report is what an import changes or leaves out (see Notes).
type Report struct {
	Servers []Server
	Whole   bool // one server as the whole server
	Renamed []Rename
	Merged  int // users of several servers with one certificate
}

// rootOnly works in root only: on a server's channel it does nothing.
const rootOnly = mumbled.Kick | mumbled.Ban | mumbled.Register | mumbled.SelfRegister | mumbled.ResetUserContent

// Plan makes the import. whole: one server as the whole server (its root
// is ours, its ids stay). Else each server is a channel under our root
// (its root's ACLs and groups there), with new ids; one certificate is
// one user, and a name taken by an earlier server's user gets a suffix.
func Plan(servers []Server, whole bool) (mumbled.Import, Report) {
	var imp mumbled.Import
	rep := Report{Servers: servers, Whole: whole}
	byHash := map[string]uint32{}
	taken := map[string]bool{}
	nextUser, nextChan := uint32(1), uint32(1)
	for _, s := range servers {
		users := map[uint32]uint32{0: 0} // SuperUser is ours too
		var mine []int                   // indexes in imp.Users from this server
		for _, u := range s.Users {
			if id, ok := byHash[u.CertHash]; ok && !whole {
				users[u.ID] = id
				rep.Merged++
				continue
			}
			name := u.Name
			for n := 2; taken[names.Key(name)]; n++ {
				name = fmt.Sprintf("%s-%d", u.Name, n)
			}
			if name != u.Name {
				rep.Renamed = append(rep.Renamed, Rename{Server: serverName(s), From: u.Name, To: name})
			}
			id := u.ID
			if !whole {
				id, nextUser = nextUser, nextUser+1
			}
			users[u.ID], byHash[u.CertHash], taken[names.Key(name)] = id, id, true
			u.ID, u.Name = id, name
			imp.Users = append(imp.Users, u)
			mine = append(mine, len(imp.Users)-1)
		}
		chans := map[uint32]uint32{0: 0}
		if !whole {
			chans[0], nextChan = nextChan, nextChan+1
		}
		for _, c := range s.Channels {
			chans[c.ID] = c.ID
			if !whole {
				chans[c.ID], nextChan = nextChan, nextChan+1
			}
		}
		root := remap(s.Root, chans, users)
		if whole {
			imp.Root = &root
		} else {
			root.Parent, root.Name = 0, serverName(s)
			imp.Channels = append(imp.Channels, root)
		}
		for _, c := range s.Channels {
			imp.Channels = append(imp.Channels, remap(c, chans, users))
		}
		for _, i := range mine {
			imp.Users[i].LastChannel = chans[imp.Users[i].LastChannel]
		}
		imp.Bans = append(imp.Bans, s.Bans...)
	}
	if whole && len(servers) == 1 {
		imp.ServerName = servers[0].Name
	}
	return imp, rep
}

func serverName(s Server) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("Server %d", s.ID)
}

// remap gives a channel its new ids, and its ACLs and groups the new user
// ids (entries of users not imported go).
func remap(c mumbled.Channel, chans, users map[uint32]uint32) mumbled.Channel {
	c.ID, c.Parent = chans[c.ID], chans[c.Parent]
	links := map[uint32]bool{}
	for l := range c.Links {
		if id, ok := chans[l]; ok {
			links[id] = true
		}
	}
	c.Links = links
	var acls []mumbled.ACL
	for _, a := range c.ACLs {
		if a.HasUser {
			id, ok := users[a.UserID]
			if !ok {
				continue
			}
			a.UserID = id
		}
		acls = append(acls, a)
	}
	c.ACLs = acls
	groups := map[string]*mumbled.Group{}
	for name, g := range c.Groups {
		ng := &mumbled.Group{Name: g.Name, Inherit: g.Inherit, Inheritable: g.Inheritable, Add: map[uint32]bool{}, Remove: map[uint32]bool{}}
		for id := range g.Add {
			if n, ok := users[id]; ok {
				ng.Add[n] = true
			}
		}
		for id := range g.Remove {
			if n, ok := users[id]; ok {
				ng.Remove[n] = true
			}
		}
		groups[name] = ng
	}
	c.Groups = groups
	return c
}

// Notes: what the admin should know, one line each.
func (r Report) Notes() string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	var sk Skipped
	for _, s := range r.Servers {
		name := serverName(s)
		if len(s.NoCert) > 0 {
			add("%s: %d users without a certificate not imported (they logged in with a password only; they register again): %s", name, len(s.NoCert), strings.Join(s.NoCert, ", "))
		}
		if s.Welcome != "" {
			add("%s: its welcome text is not imported; set MUMBLE_WELCOME for it", name)
		}
		if s.DefaultChannel {
			add("%s: Kallisti has no default channel; users arrive in root", name)
		}
		if !r.Whole {
			for _, a := range s.Root.ACLs {
				if a.Allow&rootOnly != 0 {
					add("%s: root ACL rights like Kick, Ban and Register work only on Kallisti's root, not on the server's channel; give them there", name)
					break
				}
			}
		}
		sk.Passwords += s.Skipped.Passwords
		sk.Emails += s.Skipped.Emails
		sk.UserLimits += s.Skipped.UserLimits
		sk.Listeners += s.Skipped.Listeners
	}
	if !r.Whole && len(r.Servers) > 0 {
		add("each server is a channel now: its admins manage that channel; Kallisti's admins (Server settings) manage everything")
	}
	if sk.Passwords > 0 {
		add("%d passwords not imported: those users log in with their certificate", sk.Passwords)
	}
	if sk.Emails > 0 {
		add("%d email addresses not imported (Kallisti keeps none)", sk.Emails)
	}
	if sk.UserLimits > 0 {
		add("%d channel user limits not imported (Kallisti has none)", sk.UserLimits)
	}
	if sk.Listeners > 0 {
		add("%d channel listeners not imported", sk.Listeners)
	}
	if r.Merged > 0 {
		add("%d users were on several servers with one certificate: one user each now", r.Merged)
	}
	for _, n := range r.Renamed {
		add("%s: %s is %s now (the name was taken by another server's user)", n.Server, n.From, n.To)
	}
	return strings.Join(out, "\n")
}
