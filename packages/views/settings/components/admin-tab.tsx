"use client";

import { useEffect, useState } from "react";
import { Check, Copy, Trash2, UserPlus } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { copyText } from "@multica/ui/lib/clipboard";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
  SettingsTab,
} from "./settings-layout";

export function AdminTab() {
  const [vitals, setVitals] = useState<{
    server_version: string;
    total_users: number;
    total_workspaces: number;
    signup_policy: string;
    invitation_authority: string;
    workspace_creation_policy: string;
    enforce_worktree: boolean;
  } | null>(null);

  const [signupPolicy, setSignupPolicy] = useState("invite_only");
  const [invitationAuthority, setInvitationAuthority] = useState("admins_only");
  const [workspaceCreationPolicy, setWorkspaceCreationPolicy] = useState("admins_only");
  const [savingPolicy, setSavingPolicy] = useState(false);

  // Invitations
  const [invitations, setInvitations] = useState<any[]>([]);
  const [inviteEmail, setInviteEmail] = useState("");
  const [inviteRole, setInviteRole] = useState("standard");
  const [generatingInvite, setGeneratingInvite] = useState(false);
  const [lastGeneratedUrl, setLastGeneratedUrl] = useState("");
  const [copiedUrl, setCopiedUrl] = useState(false);

  // Workspaces
  const [workspaces, setWorkspaces] = useState<any[]>([]);
  const [transferWsId, setTransferWsId] = useState("");
  const [newOwnerId, setNewOwnerId] = useState("");
  const [transferring, setTransferring] = useState(false);

  const loadData = async () => {
    try {
      const v = await api.getAdminVitals();
      setVitals(v);
      setSignupPolicy(v.signup_policy || "invite_only");
      setInvitationAuthority(v.invitation_authority || "admins_only");
      setWorkspaceCreationPolicy(v.workspace_creation_policy || "admins_only");

      const invs = await api.listInstanceInvitations();
      setInvitations(invs || []);

      const ws = await api.listAdminWorkspaces();
      setWorkspaces(ws || []);
    } catch (err) {
      console.error("Failed to load admin data", err);
    }
  };

  useEffect(() => {
    loadData();
  }, []);

  const handleSavePolicy = async () => {
    setSavingPolicy(true);
    try {
      await api.updateAdminSetting("signup_policy", {
        policy: signupPolicy,
        allowed_domains: [],
      });
      await api.updateAdminSetting("invitation_authority", {
        who_can_invite: invitationAuthority,
      });
      await api.updateAdminSetting("workspace_creation_policy", {
        policy: workspaceCreationPolicy,
        max_workspaces_per_user: 0,
      });
      toast.success("Instance policies updated successfully");
      loadData();
    } catch {
      toast.error("Failed to update instance policies");
    } finally {
      setSavingPolicy(false);
    }
  };

  const handleCreateInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!inviteEmail) return;
    setGeneratingInvite(true);
    try {
      const res = await api.createInstanceInvitation({
        email: inviteEmail,
        system_role: inviteRole,
      });
      setLastGeneratedUrl(res.invite_url);
      setInviteEmail("");
      toast.success("Instance invitation generated");
      loadData();
    } catch {
      toast.error("Failed to generate invitation");
    } finally {
      setGeneratingInvite(false);
    }
  };

  const handleRevokeInvite = async (id: string) => {
    try {
      await api.revokeInstanceInvitation(id);
      toast.success("Invitation revoked");
      loadData();
    } catch {
      toast.error("Failed to revoke invitation");
    }
  };

  const handleTransferOwnership = async (wsId: string) => {
    if (!newOwnerId) return;
    setTransferring(true);
    try {
      await api.transferWorkspaceOwnership(wsId, newOwnerId);
      toast.success("Workspace ownership transferred");
      setTransferWsId("");
      setNewOwnerId("");
      loadData();
    } catch {
      toast.error("Failed to transfer workspace ownership");
    } finally {
      setTransferring(false);
    }
  };

  return (
    <SettingsTab title="Instance Administration" description="Govern user registration, onboarding restrictions, and workspace permissions.">
      {/* Vitals & Overview */}
      <SettingsSection
        title="Instance Overview"
        description="Global health and capacity across this self-hosted Multica instance."
      >
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
          <div className="p-3 bg-muted/30 border rounded-lg">
            <div className="text-xs text-muted-foreground font-medium uppercase">Version</div>
            <div className="text-lg font-bold">{vitals?.server_version || "v0.4.32"}</div>
            <div className="text-xs text-emerald-500 font-medium">● Operational</div>
          </div>
          <div className="p-3 bg-muted/30 border rounded-lg">
            <div className="text-xs text-muted-foreground font-medium uppercase">Total Users</div>
            <div className="text-lg font-bold">{vitals?.total_users ?? 0}</div>
            <div className="text-xs text-muted-foreground">Registered accounts</div>
          </div>
          <div className="p-3 bg-muted/30 border rounded-lg">
            <div className="text-xs text-muted-foreground font-medium uppercase">Workspaces</div>
            <div className="text-lg font-bold">{vitals?.total_workspaces ?? 0}</div>
            <div className="text-xs text-muted-foreground">Admin governed</div>
          </div>
          <div className="p-3 bg-muted/30 border rounded-lg">
            <div className="text-xs text-muted-foreground font-medium uppercase">Worktree Safety</div>
            <div className="text-lg font-bold text-indigo-400">Enforced</div>
            <div className="text-xs text-muted-foreground">Isolated git runs</div>
          </div>
        </div>
      </SettingsSection>

      {/* Access & Restrictions */}
      <SettingsSection
        title="Access Policies & Restrictions"
        description="Govern user onboarding, invitations, and workspace provisioning."
      >
        <SettingsCard>
          <SettingsRow
            label="Signup Policy"
            description="Control how new users register on this instance."
          >
            <select
              value={signupPolicy}
              onChange={(e) => setSignupPolicy(e.target.value)}
              className="h-9 px-3 py-1 bg-background border rounded-md text-sm outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="invite_only">Strict Invite-Only</option>
              <option value="domain_allowlist">Corporate Domain Allowlist</option>
              <option value="open">Open Registration</option>
              <option value="closed">Completely Disabled</option>
            </select>
          </SettingsRow>

          <SettingsRow
            label="Who Can Invite Users"
            description="Determine whether only instance administrators can onboard new users."
          >
            <select
              value={invitationAuthority}
              onChange={(e) => setInvitationAuthority(e.target.value)}
              className="h-9 px-3 py-1 bg-background border rounded-md text-sm outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="admins_only">Instance Admins Only</option>
              <option value="workspace_admins">Any Workspace Admin</option>
              <option value="all_members">All Workspace Members</option>
            </select>
          </SettingsRow>

          <SettingsRow
            label="Workspace Creation Scope"
            description="Restrict workspace creation to instance administrators only."
          >
            <select
              value={workspaceCreationPolicy}
              onChange={(e) => setWorkspaceCreationPolicy(e.target.value)}
              className="h-9 px-3 py-1 bg-background border rounded-md text-sm outline-none focus:ring-1 focus:ring-primary"
            >
              <option value="admins_only">Instance Admins Only</option>
              <option value="anyone">Anyone (Self-Service)</option>
              <option value="disabled">Disabled for Everyone</option>
            </select>
          </SettingsRow>

          <div className="p-4 border-t flex justify-end">
            <Button size="sm" onClick={handleSavePolicy} disabled={savingPolicy}>
              {savingPolicy ? "Saving Policies..." : "Save Policies"}
            </Button>
          </div>
        </SettingsCard>
      </SettingsSection>

      {/* Instance Invitations */}
      <SettingsSection
        title="Instance Invitations"
        description="Invite new users to the self-hosted platform with pre-assigned roles."
      >
        <SettingsCard>
          <form onSubmit={handleCreateInvite} className="p-4 flex flex-col md:flex-row gap-3">
            <Input
              type="email"
              placeholder="colleague@acme.corp"
              value={inviteEmail}
              onChange={(e) => setInviteEmail(e.target.value)}
              className="flex-1"
              required
            />
            <select
              value={inviteRole}
              onChange={(e) => setInviteRole(e.target.value)}
              className="h-9 px-3 py-1 bg-background border rounded-md text-sm"
            >
              <option value="standard">Standard Member</option>
              <option value="instance_admin">Instance Admin</option>
            </select>
            <Button type="submit" size="sm" disabled={generatingInvite}>
              <UserPlus className="size-4 mr-1.5" />
              {generatingInvite ? "Generating..." : "Generate Invite Link"}
            </Button>
          </form>

          {lastGeneratedUrl && (
            <div className="mx-4 mb-4 p-3 bg-indigo-500/10 border border-indigo-500/20 rounded-md flex items-center justify-between gap-2">
              <div className="text-xs font-mono truncate text-indigo-300">
                {lastGeneratedUrl}
              </div>
              <Button
                variant="outline"
                size="sm"
                className="shrink-0"
                onClick={async () => {
                  await copyText(lastGeneratedUrl);
                  setCopiedUrl(true);
                  setTimeout(() => setCopiedUrl(false), 2000);
                  toast.success("Invite link copied to clipboard");
                }}
              >
                {copiedUrl ? <Check className="size-3.5 text-emerald-400 mr-1" /> : <Copy className="size-3.5 mr-1" />}
                {copiedUrl ? "Copied" : "Copy Link"}
              </Button>
            </div>
          )}

          {/* Table */}
          <div className="border-t overflow-x-auto">
            <table className="w-full text-xs text-left">
              <thead className="bg-muted/40 uppercase text-muted-foreground font-medium">
                <tr>
                  <th className="px-4 py-2.5">Invited Email</th>
                  <th className="px-4 py-2.5">System Role</th>
                  <th className="px-4 py-2.5">Invited By</th>
                  <th className="px-4 py-2.5">Status</th>
                  <th className="px-4 py-2.5 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/50">
                {invitations.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-4 py-6 text-center text-muted-foreground">
                      No active instance invitations
                    </td>
                  </tr>
                ) : (
                  invitations.map((inv) => (
                    <tr key={inv.id} className="hover:bg-muted/20">
                      <td className="px-4 py-2.5 font-medium">{inv.email}</td>
                      <td className="px-4 py-2.5">
                        <Badge variant="outline" className="text-[10px]">
                          {inv.system_role}
                        </Badge>
                      </td>
                      <td className="px-4 py-2.5 text-muted-foreground">{inv.inviter_email || "Admin"}</td>
                      <td className="px-4 py-2.5">
                        {inv.accepted_at ? (
                          <Badge variant="default" className="bg-emerald-500/15 text-emerald-400 border-emerald-500/20 text-[10px]">
                            Accepted
                          </Badge>
                        ) : inv.revoked_at ? (
                          <Badge variant="outline" className="text-muted-foreground text-[10px]">
                            Revoked
                          </Badge>
                        ) : (
                          <Badge variant="secondary" className="text-[10px]">
                            Pending
                          </Badge>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-right">
                        {!inv.accepted_at && !inv.revoked_at && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="text-destructive hover:bg-destructive/10"
                            onClick={() => handleRevokeInvite(inv.id)}
                          >
                            <Trash2 className="size-3.5" />
                          </Button>
                        )}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </SettingsCard>
      </SettingsSection>

      {/* Workspaces & Ownership */}
      <SettingsSection
        title="Instance Workspaces & Governance"
        description="Inspect all workspaces on the instance and transfer orphaned workspaces."
      >
        <SettingsCard>
          <div className="overflow-x-auto">
            <table className="w-full text-xs text-left">
              <thead className="bg-muted/40 uppercase text-muted-foreground font-medium">
                <tr>
                  <th className="px-4 py-2.5">Workspace</th>
                  <th className="px-4 py-2.5">Slug</th>
                  <th className="px-4 py-2.5">Owner</th>
                  <th className="px-4 py-2.5">Members</th>
                  <th className="px-4 py-2.5">Agents</th>
                  <th className="px-4 py-2.5 text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/50">
                {workspaces.map((ws) => (
                  <tr key={ws.id} className="hover:bg-muted/20">
                    <td className="px-4 py-2.5 font-semibold">{ws.name}</td>
                    <td className="px-4 py-2.5 font-mono text-muted-foreground">/{ws.slug}</td>
                    <td className="px-4 py-2.5">{ws.owner_email || "No Owner (Orphaned)"}</td>
                    <td className="px-4 py-2.5">{ws.member_count}</td>
                    <td className="px-4 py-2.5">{ws.agent_count}</td>
                    <td className="px-4 py-2.5 text-right">
                      {transferWsId === ws.id ? (
                        <div className="flex items-center justify-end gap-1.5">
                          <Input
                            placeholder="New Owner User ID"
                            value={newOwnerId}
                            onChange={(e) => setNewOwnerId(e.target.value)}
                            className="h-7 w-36 text-xs"
                          />
                          <Button
                            size="sm"
                            className="h-7 px-2 text-xs"
                            onClick={() => handleTransferOwnership(ws.id)}
                            disabled={transferring}
                          >
                            Confirm
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 px-2 text-xs"
                            onClick={() => setTransferWsId("")}
                          >
                            Cancel
                          </Button>
                        </div>
                      ) : (
                        <Button
                          variant="outline"
                          size="sm"
                          className="h-7 text-xs"
                          onClick={() => setTransferWsId(ws.id)}
                        >
                          Transfer Owner
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
