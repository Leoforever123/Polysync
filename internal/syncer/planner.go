package syncer

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"polysync/internal/model"
)

type Transfer = model.Transfer
type Plan = model.Plan

func BuildPlan(server, client, baseline []model.Entry, clientName string, now time.Time) Plan {
	serverMap := entryMap(server)
	clientMap := entryMap(client)
	baseMap := entryMap(baseline)
	paths := make(map[string]struct{}, len(serverMap)+len(clientMap)+len(baseMap))
	for key := range serverMap {
		paths[key] = struct{}{}
	}
	for key := range clientMap {
		paths[key] = struct{}{}
	}
	for key := range baseMap {
		paths[key] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for key := range paths {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)

	var plan Plan
	for _, filePath := range ordered {
		s, hasServer := serverMap[filePath]
		c, hasClient := clientMap[filePath]
		b, hadBase := baseMap[filePath]
		serverChanged := changed(s, hasServer, b, hadBase)
		clientChanged := changed(c, hasClient, b, hadBase)

		switch {
		case hasServer && hasClient && s.Hash == c.Hash:
			continue
		case !hadBase && hasServer && !hasClient:
			plan.ServerSends = append(plan.ServerSends, transfer(s, filePath))
		case !hadBase && !hasServer && hasClient:
			plan.ClientSends = append(plan.ClientSends, transfer(c, filePath))
		case serverChanged && !clientChanged:
			if hasServer {
				plan.ServerSends = append(plan.ServerSends, transfer(s, filePath))
			} else {
				plan.ClientDeletes = append(plan.ClientDeletes, filePath)
			}
		case !serverChanged && clientChanged:
			if hasClient {
				plan.ClientSends = append(plan.ClientSends, transfer(c, filePath))
			} else {
				plan.ServerDeletes = append(plan.ServerDeletes, filePath)
			}
		case !hasServer && !hasClient:
			continue
		default:
			addConflict(&plan, filePath, s, hasServer, c, hasClient, b, hadBase, clientName, now)
		}
	}
	return plan
}

func addConflict(plan *Plan, filePath string, server model.Entry, hasServer bool, client model.Entry, hasClient bool, baseline model.Entry, hadBaseline bool, clientName string, now time.Time) {
	detail := model.PlanConflict{Path: filePath, ServerHash: server.Hash, ClientHash: client.Hash, ServerExists: hasServer, ClientExists: hasClient}
	if hadBaseline {
		detail.BaseHash = baseline.Hash
	}
	if !hasServer {
		detail.Kind = "delete-modify"
		plan.ClientSends = append(plan.ClientSends, transfer(client, filePath))
	} else if !hasClient {
		detail.Kind = "modify-delete"
		plan.ServerSends = append(plan.ServerSends, transfer(server, filePath))
	} else {
		if hadBaseline {
			detail.Kind = "modify-modify"
		} else {
			detail.Kind = "add-add"
		}
		conflictPath := conflictName(filePath, clientName, client.Hash, now)
		detail.ConflictCopyPath = conflictPath
		clientCopy := transfer(client, conflictPath)
		clientCopy.Source = filePath
		plan.ClientSends = append(plan.ClientSends, clientCopy)
		plan.ServerSends = append(plan.ServerSends, transfer(server, filePath))
		plan.ServerSends = append(plan.ServerSends, Transfer{Source: conflictPath, Dest: conflictPath, Size: client.Size, Hash: client.Hash, Mode: client.Mode, ModTime: client.ModTime})
	}
	plan.Conflicts = append(plan.Conflicts, filePath)
	plan.ConflictDetails = append(plan.ConflictDetails, detail)
}

func changed(current model.Entry, exists bool, baseline model.Entry, hadBaseline bool) bool {
	if exists != hadBaseline {
		return true
	}
	if !exists {
		return false
	}
	return current.Hash != baseline.Hash
}

func transfer(entry model.Entry, dest string) Transfer {
	return Transfer{Source: entry.Path, Dest: dest, Size: entry.Size, Hash: entry.Hash, Mode: entry.Mode, ModTime: entry.ModTime}
}

func entryMap(entries []model.Entry) map[string]model.Entry {
	result := make(map[string]model.Entry, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry
	}
	return result
}

func conflictName(filePath, device, hash string, now time.Time) string {
	ext := path.Ext(filePath)
	stem := strings.TrimSuffix(filePath, ext)
	device = sanitizeDevice(device)
	if len(hash) > 8 {
		hash = hash[:8]
	}
	return fmt.Sprintf("%s.polysync-conflict-%s-%s-%s%s", stem, device, now.UTC().Format("20060102T150405Z"), hash, ext)
}

func sanitizeDevice(device string) string {
	var result strings.Builder
	for _, r := range device {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		}
	}
	if result.Len() == 0 {
		return "peer"
	}
	return result.String()
}
