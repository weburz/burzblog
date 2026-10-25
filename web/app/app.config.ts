export default defineAppConfig({
  ui: {
    colors: { primary: "burzyellow", secondary: "burzblue", neutral: "slate" },
    // Focus rings use the blue accent; yellow is too faint on white.
    input: { defaultVariants: { color: "secondary" } },
    textarea: { defaultVariants: { color: "secondary" } },
    // Keep titles and descriptions clear of the close button.
    slideover: { slots: { header: "pe-16 sm:pe-16" } },
    modal: { slots: { header: "pe-16 sm:pe-16" } },
    button: {
      compoundVariants: [
        { size: "md", class: "min-h-9" },
        {
          color: "secondary",
          variant: "solid",
          class: "text-white dark:text-(--bb-accent-ink)",
        },
        {
          color: "error",
          variant: "solid",
          class: "text-white dark:text-(--bb-accent-ink)",
        },
      ],
    },
  },
});
