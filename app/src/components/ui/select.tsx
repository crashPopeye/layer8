import * as React from "react"
import { ChevronDownIcon } from "lucide-react"
import { DropdownMenu, DropdownMenuContent, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { field } from "@/components/ui/input"
import { cn } from "@/lib/utils"

type Option = string | readonly [value: string, label: string]

/** A single-choice picker that looks like an Input and opens the shared menu. */
function Select({
  value,
  onValueChange,
  options,
  placeholder,
  className,
  ...props
}: Omit<React.ComponentProps<"button">, "value"> & {
  value: string
  onValueChange: (value: string) => void
  options: readonly Option[]
  placeholder?: string
}) {
  const pairs = options.map((o) => (typeof o === "string" ? ([o, o] as const) : o))
  const label = pairs.find(([v]) => v === value)?.[1]
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          data-slot="select"
          className={cn(field, "flex h-8 items-center justify-between gap-2 text-left data-[state=open]:border-ring", className)}
          {...props}
        >
          <span className={cn("truncate", label === undefined && "text-muted-foreground")}>{label ?? placeholder}</span>
          <ChevronDownIcon className="size-3.5 shrink-0 text-muted-foreground" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[var(--radix-dropdown-menu-trigger-width)]">
        <DropdownMenuRadioGroup value={value} onValueChange={onValueChange}>
          {pairs.map(([v, l]) => (
            <DropdownMenuRadioItem key={v} value={v}>
              {l}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export { Select }
