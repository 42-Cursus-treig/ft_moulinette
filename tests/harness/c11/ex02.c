#include <unistd.h>

int	ft_any(char **tab, int(*f)(char*));

static int	non_vide(char *s)
{
	if (s && *s)
		return (1);
	return (0);
}

int	main(int argc, char **argv)
{
	int	r;

	(void)argc;
	r = ft_any(argv + 1, &non_vide);
	if (r)
		write(1, "1", 1);
	else
		write(1, "0", 1);
	return (0);
}
