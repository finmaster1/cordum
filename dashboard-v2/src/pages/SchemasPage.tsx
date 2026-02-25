/*
 * DESIGN: "Control Surface" — Schemas
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useNavigate } from "react-router-dom";
import { motion } from "framer-motion";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/Button";
import { FileCode2, ArrowLeft, Construction } from "lucide-react";

export default function SchemasPage() {
  const navigate = useNavigate();
  return (
    <div className="space-y-6">
      <PageHeader
        label="Platform"
        title="Schemas"
        subtitle="Define and manage data schemas"
        actions={
          <Button variant="outline" size="sm" onClick={() => navigate(-1 as any)}>
            <ArrowLeft className="w-3 h-3 mr-1" />
            Back
          </Button>
        }
      />

      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="instrument-card p-8"
      >
        <div className="flex flex-col items-center text-center max-w-lg mx-auto">
          <div className="w-14 h-14 rounded-xl bg-cordum/10 border border-cordum/20 flex items-center justify-center text-cordum mb-5">
            <FileCode2 className="w-6 h-6" />
          </div>
          <h3 className="font-display font-bold text-lg text-foreground mb-2">Schemas</h3>
          <p className="text-sm text-muted-foreground leading-relaxed mb-6">
            Schemas define the structure of data flowing through your agent pipelines. Create validation rules, type definitions, and transformation maps.
          </p>
          <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-amber-500/10 border border-amber-500/20 text-amber-400 text-xs font-mono">
            <Construction className="w-3.5 h-3.5" />
            Under Construction
          </div>
        </div>
      </motion.div>
    </div>
  );
}
