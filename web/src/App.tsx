import { Route, Routes } from "react-router-dom";
import NewRun from "./pages/NewRun";
import RunDetail from "./pages/RunDetail";
import RunsList from "./pages/RunsList";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<RunsList />} />
      <Route path="/runs/new" element={<NewRun />} />
      <Route path="/runs/:id" element={<RunDetail />} />
    </Routes>
  );
}
