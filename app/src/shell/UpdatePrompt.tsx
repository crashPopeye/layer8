import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { dismissUpdate, installUpdate, useUpdate } from "@/lib/updater";

/** The dialog offering an available update, or reporting the result of a manual check. */
export function UpdatePrompt() {
  const { update, installing, message } = useUpdate();
  return (
    <Dialog open={!!(update || message)} onOpenChange={(open) => !open && !installing && dismissUpdate()}>
      <DialogContent>
        <DialogTitle className="text-[14px] font-medium">{message ? "Updates" : `layer8 ${update?.version} is available`}</DialogTitle>
        <DialogDescription className="text-[12px] text-muted-foreground">
          {message ?? `You have ${update?.currentVersion}. Sessions keep running: the daemon switches to the new version once none is busy.`}
        </DialogDescription>
        <div className="flex justify-end gap-2">
          {message ? (
            <Button size="sm" onClick={dismissUpdate}>
              OK
            </Button>
          ) : (
            <>
              <Button size="sm" variant="ghost" disabled={installing} onClick={dismissUpdate}>
                Later
              </Button>
              <Button size="sm" disabled={installing} onClick={installUpdate} autoFocus>
                {installing ? "Installing…" : "Install and restart"}
              </Button>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
