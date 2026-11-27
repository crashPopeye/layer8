import * as React from "react"
import { cn } from "@/lib/utils"

const field =
  "w-full min-w-0 rounded-md border border-input bg-transparent px-2.5 py-1 text-[13px] shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-invalid:border-destructive dark:bg-input/30"

function Input({ className, ...props }: React.ComponentProps<"input">) {
  return <input data-slot="input" className={cn(field, "h-8", className)} {...props} />
}

function Textarea({ className, ...props }: React.ComponentProps<"textarea">) {
  return <textarea data-slot="textarea" className={cn(field, "min-h-20 font-mono", className)} {...props} />
}

function NativeSelect({ className, ...props }: React.ComponentProps<"select">) {
  return <select data-slot="select" className={cn(field, "h-8", className)} {...props} />
}

export { field, Input, NativeSelect, Textarea }
