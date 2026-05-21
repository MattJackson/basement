import { createFileRoute } from "@tanstack/react-router";
import { adminPage } from "@/shared/layout/adminPage";
import { LifecycleRuleEditor } from "@/shared/ui/LifecycleRuleEditor";

// v0.9.0i LIFECYCLE.WIZARD — new-rule editor. Per the operator's
// "popups for 1-2 fields max" rule, the editor (4+ fields) is a
// route, not a dialog.
export const Route = createFileRoute("/admin/clusters/$cid/buckets/$id/lifecycle/new")({
  component: adminPage(NewLifecycleRulePage),
});

function NewLifecycleRulePage() {
  const { cid, id } = Route.useParams();
  return <LifecycleRuleEditor cid={cid} bid={id} />;
}
