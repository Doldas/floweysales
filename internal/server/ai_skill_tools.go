package server

import (
	"context"
	"errors"

	"floweysales/internal/ai"
	"floweysales/internal/skills"
)

// The Research Agent's skill tools read and write the skills of the user the
// run acts for (agentUser) and nobody else's. A skill is text in the user's
// own userspace; loading one grants no tool and no data. What the agent saves
// is a draft that stays out of prompts until the user enables it.

var _ ai.SkillSource = engineSource{}

func (s engineSource) skillCaller(ctx context.Context) (skills.Service, int64, error) {
	u, ok := s.e.agentUser(ctx)
	if !ok {
		return skills.Service{}, 0, errNoAgentUser
	}
	if u.ID <= 0 {
		return skills.Service{}, 0, errors.New("skills are kept in an account's files and need a signed-in account")
	}
	if s.e.db == nil {
		return skills.Service{}, 0, errors.New("skills need the database")
	}
	return skills.Service{Store: s.e.db}, u.ID, nil
}

func (s engineSource) SkillIndex(ctx context.Context) string {
	svc, owner, err := s.skillCaller(ctx)
	if err != nil {
		return ""
	}
	return svc.Index(ctx, owner)
}

func (s engineSource) UseSkill(ctx context.Context, name, file string) (any, error) {
	svc, owner, err := s.skillCaller(ctx)
	if err != nil {
		return nil, err
	}
	return svc.Use(ctx, owner, name, file)
}

func (s engineSource) SaveSkill(ctx context.Context, name, description, body, kind string) (any, error) {
	svc, owner, err := s.skillCaller(ctx)
	if err != nil {
		return nil, err
	}
	p, err := svc.SaveDraft(ctx, owner, skills.Entry{Name: name, Description: description, Body: body, Type: kind})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"saved": p, "status": "draft",
		"note": "Saved as a draft. It is not used until the user reviews it in Files and removes the status: draft line. Tell the user where it is.",
	}, nil
}
