# Hybrid Workflows Operator
# This is a dev only implementation not production ready!!! and is tailored to Minikube with docker driver.
	Setup Guide:
	    Local Host Ubuntu Setup:
	        requirements:
	            Docker installed and working
	            Minikube installed with docker driver
	            kubectl installed
	            git installed
	            curl installed
	            ssh-keygen available
	            start minikube
	            run: bash scripts/setup.sh
	    DevContainer or Github Codespaces:
	            start minikube
	            run bash scripts/setup.sh
	    setup.sh will:
	        install ArgoCD
	        apply the root ArgoCD application to bootstrap the platform stack and Operator
	        generate or reuse local dev credentials for ArgoCD and MinIO and save them outside Git
	        create the MinIO credentials secret directly in the cluster instead of storing it in the GitOps repo
	        install or reuse the dummy Slurm container
	        upload the example datasets to the MinIO bucket my-bucket
	        create the SSH key and Kubernetes secrets used by the Slurm integration
	        when running in a devcontainer or Codespaces, run bash scripts/port-forward-uis.sh and then use localhost or the Codespaces PORTS tab
