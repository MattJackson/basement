import { createFileRoute } from "@tanstack/react-router";
import { adminPage } from "@/shared/layout/adminPage";
import { LifecycleRuleEditor } from "@/shared/ui/LifecycleRuleEditor";

// v0.9.0i LIFECYCLE.WIZARD — edit-existing-rule route. Mirrors the
// new.tsx route; the LifecycleRuleEditor hydrates from the bucket's
// existing policy when ruleId is set.
export const Route = createFileRoute(
  "/admin/clusters/$cid/buckets/$id/lifecycle/$ruleId/edit",
)({
  component: adminPage(EditLifecycleRulePage),
});

function EditLifecycleRulePage() {
  const { cid, id, ruleId } = Route.useParams();
  return <LifecycleRuleEditor cid={cid} bid={id} ruleId={ruleId} />;
}
