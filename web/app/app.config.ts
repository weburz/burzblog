export default defineAppConfig({
  ui: {
    colors: { primary: "burzyellow", secondary: "burzblue", neutral: "slate" },
    button: {
      compoundVariants: [
        { color: "secondary", variant: "solid", class: "text-white" },
        { color: "error", variant: "solid", class: "text-white" },
      ],
    },
  },
});
