package store

import (
	"context"
	"testing"

	"github.com/t0mer/mashov2go/internal/family"
)

func parent(name string) family.Member {
	return family.Member{Role: family.RoleParent, Name: name, Language: "he", Channel: family.ChannelWhatsApp,
		WhatsAppPhone: "972501111111", QuietStart: "22:30", QuietEnd: "06:30", PlanTime: "19:30", Active: true}
}

func child(name string) family.Member {
	return family.Member{Role: family.RoleChild, Name: name, Language: "he", Channel: family.ChannelWhatsApp,
		WhatsAppPhone: "972502222222", QuietStart: "21:00", QuietEnd: "07:00", PlanTime: "19:30",
		PlanEnabled: true, IncludeHomeworkToday: true, Active: true}
}

func TestMemberCRUDAndSoftDelete(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	p, err := s.CreateMember(ctx, parent("Tomer"))
	if err != nil || p.ID == 0 {
		t.Fatalf("create: %+v %v", p, err)
	}
	p.Name = "Tomer K"
	if err := s.UpdateMember(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMember(ctx, p.ID)
	if err != nil || got.Name != "Tomer K" || got.WhatsAppPhone != "972501111111" {
		t.Fatalf("after update: %+v %v", got, err)
	}
	if err := s.DeleteMember(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	all, _ := s.ListMembers(ctx)
	if len(all) != 0 {
		t.Fatalf("deleted member still listed: %+v", all)
	}
}

func TestChildLinkRecipientsAndMutes(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, Account{Name: "a", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "x"})
	kid, _ := s.CreateMember(ctx, child("Noa"))
	par, _ := s.CreateMember(ctx, parent("Tomer"))

	if err := s.LinkChild(ctx, kid.ID, acc.ID, "guid-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkChild(ctx, par.ID, acc.ID, "guid-1"); err == nil {
		t.Fatal("a student must not be linked twice")
	}
	found, err := s.ChildMemberFor(ctx, acc.ID, "guid-1")
	if err != nil || found.ID != kid.ID {
		t.Fatalf("ChildMemberFor = %+v, %v", found, err)
	}
	if accID, guid, ok, err := s.MemberLink(ctx, kid.ID); err != nil || !ok || accID != acc.ID || guid != "guid-1" {
		t.Fatalf("MemberLink = %d %q %v %v", accID, guid, ok, err)
	}
	if _, _, ok, err := s.MemberLink(ctx, par.ID); err != nil || ok {
		t.Fatalf("unlinked member: ok=%v err=%v", ok, err)
	}

	if err := s.SetRecipient(ctx, kid.ID, family.CatGrades, family.ToBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecipient(ctx, kid.ID, family.CatGrades, family.ToChild); err != nil {
		t.Fatal(err)
	}
	recips, _ := s.Recipients(ctx, kid.ID)
	if recips[family.CatGrades] != family.ToChild || len(recips) != 1 {
		t.Fatalf("recipients = %v", recips)
	}

	if err := s.SetMute(ctx, par.ID, kid.ID, family.CatBehave, true); err != nil {
		t.Fatal(err)
	}
	if muted, _ := s.IsMuted(ctx, par.ID, kid.ID, family.CatBehave); !muted {
		t.Fatal("mute not stored")
	}
	if err := s.SetMute(ctx, par.ID, kid.ID, family.CatBehave, false); err != nil {
		t.Fatal(err)
	}
	if muted, _ := s.IsMuted(ctx, par.ID, kid.ID, family.CatBehave); muted {
		t.Fatal("unmute did not clear")
	}

	if err := s.DeleteMember(ctx, kid.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChildMemberFor(ctx, acc.ID, "guid-1"); err == nil {
		t.Fatal("deleted child still resolves")
	}
}

func TestUnlinkAndResetRecipients(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	acc, _ := s.CreateAccount(ctx, Account{Name: "a", Semel: 1, Year: 2027, Username: "u", DeviceUUID: "x"})
	kid, _ := s.CreateMember(ctx, child("Noa"))
	if err := s.LinkChild(ctx, kid.ID, acc.ID, "guid-9"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlinkChild(ctx, kid.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.MemberLink(ctx, kid.ID); ok {
		t.Fatal("link still present after unlink")
	}
	if err := s.SetRecipient(ctx, kid.ID, family.CatGrades, family.ToBoth); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetRecipients(ctx, kid.ID); err != nil {
		t.Fatal(err)
	}
	if recips, _ := s.Recipients(ctx, kid.ID); len(recips) != 0 {
		t.Fatalf("overrides left after reset: %v", recips)
	}
}

func TestReplaceMutesSetsExactlyTheList(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	par, _ := s.CreateMember(ctx, parent("Tomer"))
	kid, _ := s.CreateMember(ctx, child("Noa"))
	kid2, _ := s.CreateMember(ctx, child("Dana"))

	if err := s.ReplaceMutes(ctx, par.ID, []Mute{{kid.ID, family.CatBehave}, {kid2.ID, family.CatMail}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceMutes(ctx, par.ID, []Mute{{kid.ID, family.CatGrades}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListMutes(ctx, par.ID)
	if len(got) != 1 || got[0].ChildID != kid.ID || got[0].Category != family.CatGrades {
		t.Fatalf("mutes after replace = %+v", got)
	}
}
